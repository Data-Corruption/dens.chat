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
import android.os.Build
import android.os.IBinder
import android.os.SystemClock
import java.io.IOException
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

// DensService is the foreground service Dens runs under (M12): it starts
// Dens with its data key, starts it again when it ends, but for a start
// Dens refuses, and keeps the shell's stream with it, which says where the
// page is. A foreground service of the specialUse type holds the dens'
// connections while the app isn't on screen.
class DensService : Service() {
    companion object {
        private const val NOTIFICATION = 1
        private const val CHANNEL = "service"

        // What the page's activity shows: the page's address, once Dens has
        // said it, or why Dens doesn't run.
        @Volatile
        var page: String? = null
            private set

        @Volatile
        var problem: String? = null
            private set

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
    }

    @Volatile private var stopping = false
    @Volatile private var process: Process? = null
    @Volatile private var stream: Control.Stream? = null
    private var runner: Thread? = null
    private var linker: Thread? = null
    private val retry = LinkedBlockingQueue<Unit>()

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
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
        val open = PendingIntent.getActivity(this, 0, Intent(this, PageActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val n = Notification.Builder(this, CHANNEL)
            .setSmallIcon(Dens.drawable(this, "notification"))
            .setContentTitle(applicationInfo.loadLabel(packageManager))
            .setContentText("Keeps your dens connected")
            .setContentIntent(open)
            .setOngoing(true)
            .build()
        try {
            if (Build.VERSION.SDK_INT >= 34) {
                startForeground(NOTIFICATION, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
            } else {
                startForeground(NOTIFICATION, n)
            }
        } catch (e: Exception) {
            Dens.log(this, "service: Android refused the foreground service: ${e.javaClass.simpleName}")
        }
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
                    if (msg.optString("t") == "hello") {
                        val d = msg.optJSONObject("d")
                        page = d?.optString("page")?.takeIf { it.startsWith("http://localhost:") }
                        problem = null
                        Dens.log(this, "service: Dens ${d?.optString("version")} is up")
                        changed()
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
