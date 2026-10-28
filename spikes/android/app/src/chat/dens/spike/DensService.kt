package chat.dens.spike

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.AudioManager
import android.media.AudioRecordingConfiguration
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.SystemClock
import java.io.File

// DensService is the foreground service Dens runs under, with the probe's
// heartbeat beside it: it starts both, starts each again when it dies, and
// adds the microphone type while the page records, which keeps a call's
// microphone open with the screen off.
class DensService : Service() {
    companion object {
        const val START = "start"
        const val STOP = "stop"
        const val MIC_ON = "mic-on"
        const val FEATURES = "features"
        // ROUTE moves a call's sound to its "route" extra, from the
        // notification's buttons, which leave the page and its call open.
        const val ROUTE = "route"
        // BACKGROUND runs the children under a plain started service, with
        // no foreground service: what Android does to them then.
        const val BACKGROUND = "background"
        private const val NOTIFICATION = 1

        @Volatile private var current: DensService? = null

        // status describes the children as they are now.
        val status: String
            get() = current?.describe() ?: "service not running"

        fun start(ctx: Context, action: String = START) {
            val i = Intent(ctx, DensService::class.java).setAction(action)
            if (action == BACKGROUND) ctx.startService(i) else ctx.startForegroundService(i)
        }

        fun stop(ctx: Context) {
            ctx.startService(Intent(ctx, DensService::class.java).setAction(STOP))
        }

        fun channels(ctx: Context) {
            val nm = ctx.getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(NotificationChannel("service", "Connection", NotificationManager.IMPORTANCE_LOW))
            nm.createNotificationChannel(NotificationChannel("messages", "Messages", NotificationManager.IMPORTANCE_HIGH))
            nm.createNotificationChannel(NotificationChannel("downloads", "Downloads", NotificationManager.IMPORTANCE_DEFAULT))
        }
    }

    // A child the service keeps running.
    private inner class Child(val name: String, val build: () -> ProcessBuilder) {
        @Volatile var process: Process? = null
        @Volatile var started = 0L
        var restarts = 0
        var lastExit = "none"

        fun start() {
            if (stopping || process != null) return
            val pb = build()
            pb.redirectErrorStream(true)
            pb.redirectOutput(ProcessBuilder.Redirect.appendTo(File(Spike.dir(this@DensService), "$name.out")))
            val p = try {
                pb.start()
            } catch (e: Exception) {
                Spike.log(this@DensService, "$name: start failed: $e")
                lastExit = "start failed: $e"
                schedule()
                return
            }
            p.outputStream.close()
            process = p
            started = SystemClock.elapsedRealtime()
            Spike.log(this@DensService, "$name: started, pid ${pid(p)}")
            Thread {
                val code = p.waitFor()
                val up = (SystemClock.elapsedRealtime() - started) / 1000
                // Java reports a child a signal ended as 128 plus the signal.
                lastExit = if (code > 128) "signal ${code - 128} after $up s" else "exit $code after $up s"
                Spike.log(this@DensService, "$name: ended, $lastExit")
                process = null
                update()
                if (!stopping) schedule()
            }.start()
            update()
        }

        private fun schedule() {
            restarts++
            val delay = minOf(60_000L, 2_000L shl minOf(restarts, 5))
            main.postDelayed({ start() }, delay)
        }

        fun stop() {
            process?.destroy()
        }

        fun describe(): String {
            val p = process
            return if (p != null) "$name pid ${pid(p)}, up ${(SystemClock.elapsedRealtime() - started) / 1000} s, restarts $restarts"
            else "$name not running, last $lastExit, restarts $restarts"
        }
    }

    private val main = Handler(Looper.getMainLooper())
    @Volatile private var stopping = false
    private var mic = false
    private var routed = ""
    private val dens = Child("dens") { Spike.dens(this, "service", "run", "--port", Spike.PAGE_PORT.toString()) }
    private val heartbeat = Child("heartbeat") { Spike.command(this, "libprobe.so", "heartbeat") }
    private val features = Child("features") { Spike.command(this, "libprobe.so", "serve", Spike.FEATURE_PORT.toString()) }
    private var micOff: Runnable? = null

    private val recordings = object : AudioManager.AudioRecordingCallback() {
        override fun onRecordingConfigChanged(configs: MutableList<AudioRecordingConfiguration>) {
            Spike.log(this@DensService, "recordings: ${configs.size}")
            micOff?.let { main.removeCallbacks(it) }
            if (configs.isEmpty() && mic) {
                // The page stopped recording: drop the microphone type once
                // it's been quiet a while, as a call ends.
                val r = Runnable {
                    mic = false
                    getSystemService(AudioManager::class.java).clearCommunicationDevice()
                    routed = ""
                    PageActivity.release()
                    foreground()
                }
                micOff = r
                main.postDelayed(r, 10_000)
            }
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        channels(this)
        current = this
        getSystemService(AudioManager::class.java).registerAudioRecordingCallback(recordings, main)
        Spike.log(this, "service: created")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Spike.log(this, "service: command ${intent?.action ?: "(restarted by Android)"}")
        if (intent?.action == ROUTE) {
            // Only the notification changes: starting the foreground again
            // could lose the microphone type mid-call.
            routed = Audio.route(this, intent.getStringExtra("route") ?: "clear")
            Spike.log(this, "route: $routed")
            update()
            return START_STICKY
        }
        when (intent?.action) {
            STOP -> {
                stopping = true
                for (c in listOf(dens, heartbeat, features)) c.stop()
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
                return START_NOT_STICKY
            }
            MIC_ON -> mic = true
        }
        stopping = false
        if (intent?.action == BACKGROUND) {
            Spike.log(this, "service: running without a foreground service")
        } else {
            foreground()
        }
        dens.start()
        heartbeat.start()
        if (intent?.action == FEATURES) features.start()
        return START_STICKY
    }

    private fun foreground() {
        val remote = Spike.prefs(this).getString("fgs", "specialUse") == "remoteMessaging"
        var type = 0
        if (Build.VERSION.SDK_INT >= 34) {
            type = if (remote) ServiceInfo.FOREGROUND_SERVICE_TYPE_REMOTE_MESSAGING else ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
        }
        if (mic) type = type or ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
        try {
            if (type == 0) startForeground(NOTIFICATION, notification()) else startForeground(NOTIFICATION, notification(), type)
            Spike.log(this, "service: foreground, type $type (${if (remote) "remoteMessaging" else "specialUse"}${if (mic) " + microphone" else ""})")
        } catch (e: Exception) {
            Spike.log(this, "service: startForeground refused: $e")
            if (mic) {
                mic = false
                foreground()
            }
        }
        update()
    }

    private fun notification(): Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, PageActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, DensService::class.java).setAction(STOP), PendingIntent.FLAG_IMMUTABLE)
        val text = if (mic) routed.ifEmpty { Audio.describe(this) } else dens.describe()
        val b = Notification.Builder(this, "service")
            .setSmallIcon(android.R.drawable.stat_notify_chat)
            .setContentTitle(if (mic) "Dens Spike: in a call" else "Dens Spike: connected")
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setContentIntent(open)
            .setOngoing(true)
        if (mic) {
            // A call's buttons: where its sound goes, and Android's choice.
            for ((i, route) in listOf("speaker", "earpiece", "clear").withIndex()) {
                val go = PendingIntent.getService(
                    this, 3 + i, Intent(this, DensService::class.java).setAction(ROUTE).putExtra("route", route), PendingIntent.FLAG_IMMUTABLE
                )
                b.addAction(Notification.Action.Builder(null, if (route == "clear") "Auto" else route.replaceFirstChar { it.uppercase() }, go).build())
            }
        } else {
            b.addAction(Notification.Action.Builder(null, "Stop", stop).build())
        }
        return b.build()
    }

    private fun describe(): String =
        listOf(dens, heartbeat, features).joinToString("\n") { it.describe() } + if (mic) "\nmicrophone type on" else ""

    // update shows the children in the service's notification.
    private fun update() {
        if (!stopping) getSystemService(android.app.NotificationManager::class.java).notify(NOTIFICATION, notification())
    }

    override fun onTrimMemory(level: Int) {
        Spike.log(this, "service: trim memory $level")
    }

    override fun onTimeout(startId: Int, fgsType: Int) {
        Spike.log(this, "service: timed out, type $fgsType")
    }

    override fun onDestroy() {
        Spike.log(this, "service: destroyed")
        stopping = true
        current = null
        for (c in listOf(dens, heartbeat, features)) c.stop()
        getSystemService(AudioManager::class.java).unregisterAudioRecordingCallback(recordings)
        super.onDestroy()
    }
}

// pid reads a child's PID: Process.pid() where the runtime has it, and
// otherwise the "pid=" Android's process class prints.
fun pid(p: Process): Long = try {
    Process::class.java.getMethod("pid").invoke(p) as Long
} catch (e: Throwable) {
    Regex("pid=(\\d+)").find(p.toString())?.groupValues?.get(1)?.toLong() ?: -1
}
