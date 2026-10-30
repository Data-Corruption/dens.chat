package chat.dens.shell

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.content.pm.ServiceInfo
import android.media.AudioDeviceCallback
import android.media.AudioDeviceInfo
import android.media.AudioManager
import android.media.AudioRecordingConfiguration
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.SystemClock
import org.json.JSONObject
import java.io.IOException
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.Executors
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

// DensService is the foreground service Dens runs under (M12): it starts
// Dens with its data key, starts it again when it ends, but for a start
// Dens refuses, and keeps the shell's stream with it, which says where the
// page is, and carries the member's notifications, which the shell posts
// (M12.3), and the updates Dens readies, which it hands to Android's
// installer (M12.4). A foreground service of the specialUse type holds the dens'
// connections while the app isn't on screen, and of the microphone's type
// too while the page has the microphone, as in a call, whose sound it
// routes.
class DensService : Service() {
    companion object {
        private const val NOTIFICATION = 1
        private const val CHANNEL = "service"
        // The service's actions besides starting: the page takes the
        // microphone (CALL), and a button on the call's notification moves
        // its sound (ROUTE, with the route in an extra of that name).
        private const val CALL = "call"
        private const val ROUTE = "route"
        // How long after the page stops recording its call ends: it may
        // record again first, as when the member picks another microphone.
        private const val CALL_ENDS_MS = 10_000L
        // How long after a headset comes or goes the shell routes a call:
        // the WebView's Chromium routes it by its own lights then, and the
        // last to say is where it plays.
        private const val SETTLE_MS = 500L

        // What the page's activity shows: the page's address, once Dens has
        // said it, or why Dens doesn't run.
        @Volatile
        var page: String? = null
            private set

        @Volatile
        var problem: String? = null
            private set

        // calling is whether the page has the microphone, as in a call: from
        // the shell's granting it until the page has stopped recording for
        // CALL_ENDS_MS. The service has the microphone's type then, and the
        // activity holds the page as shown.
        @Volatile
        var calling = false
            private set

        // notifying is whether the member turned any notifications on, as
        // Dens last said (M12.3), which the activity asks Android to let it
        // post; front is whether the page's activity shows, which Dens
        // hears, since the activity holds the page as shown through a call.
        @Volatile
        var notifying = false
            private set

        @Volatile
        private var front = false

        // isFront is whether the page's activity shows, which Android needs
        // of an app that starts its installer's confirmation (M12.4).
        val isFront get() = front

        // The shell's stream with Dens, while it's open.
        @Volatile
        private var stream: Control.Stream? = null

        private val watchers = CopyOnWriteArraySet<() -> Unit>()

        fun watch(w: () -> Unit) = watchers.add(w)
        fun unwatch(w: () -> Unit) = watchers.remove(w)
        private fun changed() = watchers.forEach { it() }

        fun prefs(ctx: Context): SharedPreferences = ctx.getSharedPreferences("shell", Context.MODE_PRIVATE)

        // start starts the service, and Dens under it, or tries Dens again
        // after a start it refused. Once it has run, the service starts
        // again as the phone does, and after each update.
        fun start(ctx: Context) {
            prefs(ctx).edit().putBoolean("running", true).apply()
            ctx.startForegroundService(Intent(ctx, DensService::class.java))
        }

        // inFront tells Dens whether the page's activity shows (M12.3): a
        // page held as shown for a call isn't on screen otherwise, and
        // Dens notifies the member of what it shows only while it isn't.
        fun inFront(on: Boolean) {
            front = on
            tell("front", JSONObject().put("on", on))
        }

        // tellAllowed tells Dens whether Android lets the app post the
        // member's notifications, which the page's settings say.
        fun tellAllowed(ctx: Context) = tell("notifies", JSONObject().put("on", Notes.allowed(ctx)))

        // tell sends Dens a line, in turn with the others, off the thread
        // that asks.
        private val teller = Executors.newSingleThreadExecutor()

        private fun tell(type: String, data: JSONObject) {
            teller.execute {
                try {
                    stream?.send(type, data)
                } catch (e: IOException) {
                    // The stream's reader sees it closed, and opens another.
                }
            }
        }

        // call gives the service the microphone's type, which Android needs
        // to let the page record off screen, as the shell grants the page
        // the microphone.
        fun call(ctx: Context) {
            if (!calling) {
                calling = true
                changed()
            }
            ctx.startForegroundService(Intent(ctx, DensService::class.java).setAction(CALL))
        }
    }

    @Volatile private var stopping = false
    @Volatile private var process: Process? = null
    private var runner: Thread? = null
    private var linker: Thread? = null
    private val retry = LinkedBlockingQueue<Unit>()
    private val main = Handler(Looper.getMainLooper())
    // The recordings the page's call has made, by session; where the member
    // chose its sound plays, if they did; and where Android last said it
    // plays.
    private var recorded = emptySet<Int>()
    private var chosen: Audio.Route? = null
    private var on: Audio.Route? = null

    // The page records through the WebView, in the app's process, and
    // Android says so here. It names no app, but a phone records for one
    // app at a time. The WebView routes the call its own way as each
    // recording opens, the first and any after, as when the member picks
    // another microphone, so the shell routes it after.
    private val recordings = object : AudioManager.AudioRecordingCallback() {
        override fun onRecordingConfigChanged(configs: MutableList<AudioRecordingConfiguration>) {
            if (!calling) return
            main.removeCallbacks(callEnds)
            if (configs.isEmpty()) main.postDelayed(callEnds, CALL_ENDS_MS)
            val sessions = configs.map { it.clientAudioSessionId }.toSet()
            if ((sessions - recorded).isNotEmpty()) route(chosen ?: Audio.choose(this@DensService), if (recorded.isEmpty()) "as the call starts" else "as the page records again")
            recorded = recorded + sessions
        }
    }

    // A headset that connects during a call takes it, as on a phone call.
    // A headset can show as more than one device, which come one by one.
    private val headsetCame = Runnable {
        chosen = null
        route(Audio.choose(this), "as a headset connected")
    }
    private val devices = object : AudioDeviceCallback() {
        override fun onAudioDevicesAdded(added: Array<out AudioDeviceInfo>) {
            val headset = added.any { d -> Audio.routeOf(d)?.headset == true }
            if (!calling || recorded.isEmpty() || !headset) return
            main.removeCallbacks(headsetCame)
            main.postDelayed(headsetCame, SETTLE_MS)
        }
    }

    // The notification follows where the call's sound plays, whoever moved
    // it. Android puts a call whose headset went on the earpiece, or on
    // nothing; it goes where it would by itself instead.
    private val moved = AudioManager.OnCommunicationDeviceChangedListener { d ->
        if (!calling) return@OnCommunicationDeviceChangedListener
        val was = on
        on = Audio.routeOf(d)
        Dens.log(this, "audio: Android plays the call ${on?.where ?: "where it would by itself"}")
        update()
        if (was?.headset == true && on != was) {
            main.postDelayed({
                if (calling && was !in Audio.available(this)) {
                    if (chosen == was) chosen = null
                    route(chosen ?: Audio.choose(this), "as its headset went")
                }
            }, SETTLE_MS)
        }
    }

    // callEnds ends the page's call once it has stopped recording a while:
    // the service drops the microphone's type, and the page is seen as it
    // is again.
    private val callEnds = Runnable {
        Dens.log(this, "service: the page stopped recording, so its call is over")
        calling = false
        recorded = emptySet()
        chosen = null
        on = null
        main.removeCallbacks(headsetCame)
        Audio.clear(this)
        foreground()
        changed()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        val am = getSystemService(AudioManager::class.java)
        am.registerAudioRecordingCallback(recordings, main)
        am.registerAudioDeviceCallback(devices, main)
        am.addOnCommunicationDeviceChangedListener(mainExecutor, moved)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            // Only the notification changes: the service is in front
            // already, and Android may not let a service take the
            // microphone's type again from the background.
            ROUTE -> {
                val to = Audio.Route.entries.firstOrNull { it.name == intent.getStringExtra(ROUTE) }
                if (calling && to != null) {
                    chosen = to
                    route(to, "as the member asked")
                }
                return START_STICKY
            }
            CALL -> {
                // A page that never records, as when its microphone wouldn't
                // open, has no call; one already recording stays in it.
                main.removeCallbacks(callEnds)
                if (getSystemService(AudioManager::class.java).activeRecordingConfigurations.isEmpty()) main.postDelayed(callEnds, CALL_ENDS_MS)
                Dens.log(this, "service: the page has the microphone")
            }
        }
        foreground()
        stopping = false
        if (runner?.isAlive != true) {
            problem = null
            changed()
            runner = Thread({ keepRunning() }, "dens").apply { start() }
        } else {
            // The app opened, or its Try again, cuts a pause short.
            retry.offer(Unit)
        }
        if (linker?.isAlive != true) linker = Thread({ keepLinked() }, "dens-stream").apply { start() }
        return START_STICKY
    }

    private fun foreground() {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, "Connection", NotificationManager.IMPORTANCE_LOW).apply { setShowBadge(false) }
        )
        // Android 14 and later need the service's type said; the
        // microphone's keeps the page's recording going off screen.
        val base = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        val mic = if (calling) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE else 0
        try {
            startForeground(NOTIFICATION, notification(), base or mic)
        } catch (e: Exception) {
            Dens.log(this, "service: Android refused the foreground service${if (mic != 0) " with the microphone's type" else ""}: ${e.javaClass.simpleName}")
            // The call goes on while the app is on screen.
            if (mic != 0) {
                try {
                    startForeground(NOTIFICATION, notification(), base)
                } catch (e: Exception) {
                    Dens.log(this, "service: Android refused the foreground service: ${e.javaClass.simpleName}")
                }
            }
        }
    }

    // notification is the service's: that it keeps the dens connected, or
    // in a call, where the call's sound plays, with a button for each other
    // place it may.
    private fun notification(): Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, PageActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        // A group of its own, so Android doesn't bundle it with the app's
        // other quiet notifications, whose taps the bundle's would take.
        val b = Notification.Builder(this, CHANNEL)
            .setSmallIcon(Dens.drawable(this, "notification"))
            .setGroup(CHANNEL)
            .setContentTitle(applicationInfo.loadLabel(packageManager))
            .setContentIntent(open)
            .setOngoing(true)
        if (!calling) return b.setContentText("Keeps your dens connected").build()
        val now = Audio.current(this)
        b.setContentText(if (now == null) "In a call" else "In a call, ${now.where}")
        val here = Audio.available(this)
        for (r in Audio.Route.entries) {
            if (r == now || r !in here) continue
            val go = Intent(this, DensService::class.java).setAction(ROUTE).putExtra(ROUTE, r.name)
            b.addAction(Notification.Action.Builder(null, r.label, PendingIntent.getService(this, 1 + r.ordinal, go, PendingIntent.FLAG_IMMUTABLE)).build())
        }
        return b.build()
    }

    // update shows the service's notification as things are now.
    private fun update() {
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION, notification())
    }

    // route has the page's call play on route, as why says.
    private fun route(to: Audio.Route?, why: String) {
        if (to == null || !calling) return
        val ok = Audio.set(this, to)
        Dens.log(this, "audio: the call plays ${to.where} $why${if (ok) "" else ", which Android refused"}")
        update()
    }

    // keepRunning keeps Dens running: it starts Dens with its key, and when
    // it ends, starts it again after a pause that grows while it keeps
    // ending soon after it starts, and says why once it has ended so three
    // times. A start Dens refuses, or a key the shell doesn't have, isn't
    // tried again until the app is opened again.
    private fun keepRunning() {
        var pause = 2_000L
        var soon = 0
        while (!stopping) {
            val started = SystemClock.elapsedRealtime()
            // Why the shell couldn't start Dens, where Dens never ran to say.
            var why = ""
            val code = try {
                runOnce()
            } catch (e: Keys.Lost) {
                return fail(e.message ?: "Dens's key is gone.")
            } catch (e: InterruptedException) {
                return
            } catch (e: Exception) {
                why = "${e.javaClass.simpleName}: ${e.message}"
                Dens.log(this, "service: couldn't start Dens: $why")
                -1
            }
            if (stopping) return
            if (code == Dens.EXIT_REFUSED) {
                return fail("Dens didn't start:\n${Dens.lastWords(this)}")
            }
            if (SystemClock.elapsedRealtime() - started > 60_000) {
                pause = 2_000L
                soon = 0
            } else if (++soon >= 3) {
                problem = "Dens keeps stopping, and the app keeps starting it again:\n${why.ifEmpty { Dens.lastWords(this) }}"
                changed()
            }
            Dens.log(this, "service: Dens ended (${exit(code)}); starting it again in ${pause / 1000} s")
            try {
                retry.clear()
                retry.poll(pause, TimeUnit.MILLISECONDS)
            } catch (e: InterruptedException) {
                return
            }
            pause = minOf(pause * 2, 60_000L)
        }
    }

    // runOnce runs Dens until it ends, handing it its key on its standard
    // input, and returns its exit status.
    private fun runOnce(): Int {
        val pb = ProcessBuilder(Dens.binary(this), "service", "run")
            .directory(filesDir)
            .redirectErrorStream(true)
            .redirectOutput(ProcessBuilder.Redirect.appendTo(Dens.freshOutput(this)))
        pb.environment().putAll(Dens.env(this))
        val key = Keys.dataKey(this)
        val p = try {
            pb.start()
        } catch (e: IOException) {
            key.fill(0)
            throw e
        }
        process = p
        try {
            p.outputStream.use { it.write(key) }
        } catch (e: IOException) {
            // Dens ended before it read its key; its status says why.
        } finally {
            key.fill(0)
        }
        Dens.log(this, "service: Dens started")
        try {
            return p.waitFor()
        } finally {
            process = null
            page = null
            changed()
        }
    }

    private fun fail(why: String) {
        Dens.log(this, "service: Dens doesn't run: ${why.lines().first()}")
        problem = why
        changed()
    }

    // keepLinked keeps the shell's stream with Dens while Dens runs: it opens it
    // once Dens answers, and again whenever it closes.
    private fun keepLinked() {
        while (!stopping && runner?.isAlive == true) {
            val s = if (process != null) {
                try {
                    Control.open(this)
                } catch (e: IOException) {
                    null
                }
            } else {
                null
            }
            if (s == null) {
                try {
                    Thread.sleep(250)
                } catch (e: InterruptedException) {
                    return
                }
                continue
            }
            stream = s
            try {
                while (true) {
                    val msg = s.next() ?: break
                    val d = msg.optJSONObject("d")
                    when (msg.optString("t")) {
                        "hello" -> {
                            page = d?.optString("page")?.takeIf { it.startsWith("http://localhost:") }
                            problem = null
                            // Dens says again what update is ready, if any.
                            Updates.forget(this)
                            Dens.log(this, "service: Dens ${d?.optString("version")} is up")
                            // What Dens needs to know to notify the member
                            // where it should.
                            inFront(front)
                            tellAllowed(this)
                            changed()
                        }
                        "notify" -> if (d != null) Notes.post(this, d)
                        "unnotify" -> {
                            val keys = d?.optJSONArray("keys")
                            if (keys != null) Notes.cancel(this, List(keys.length()) { keys.optString(it) })
                        }
                        "notifying" -> {
                            notifying = d?.optBoolean("on") == true
                            changed()
                        }
                        // An update Dens downloaded and checked (M12.4), and
                        // the member asking the page to install it.
                        "update" -> if (d != null) Updates.offer(this, d.optString("version"), d.optString("apk"))
                        "install" -> if (d != null) Updates.install(this, d.optString("version"), d.optString("apk"))
                    }
                }
            } finally {
                s.close()
                stream = null
            }
            Dens.log(this, "service: the stream with Dens closed")
        }
    }

    private fun exit(code: Int) = when {
        code > 128 -> "signal ${code - 128}"
        code < 0 -> "didn't start"
        else -> "exit $code"
    }

    override fun onDestroy() {
        Dens.log(this, "service: stopping")
        stopping = true
        val am = getSystemService(AudioManager::class.java)
        am.unregisterAudioRecordingCallback(recordings)
        am.unregisterAudioDeviceCallback(devices)
        am.removeOnCommunicationDeviceChangedListener(moved)
        main.removeCallbacksAndMessages(null)
        stream?.close()
        // Dens stops on SIGTERM, as under systemd.
        process?.destroy()
        runner?.interrupt()
        linker?.interrupt()
        super.onDestroy()
    }
}

// StartReceiver starts the service as the phone starts, and after each
// update, which ends the app's processes, once the app has run.
class StartReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        if (!DensService.prefs(ctx).getBoolean("running", false)) return
        Dens.log(ctx, "start: ${intent.action?.substringAfterLast('.')}")
        try {
            DensService.start(ctx)
        } catch (e: Exception) {
            Dens.log(ctx, "start: Android refused the service: ${e.javaClass.simpleName}")
        }
    }
}
