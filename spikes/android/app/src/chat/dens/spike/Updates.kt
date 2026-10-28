package chat.dens.spike

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Build
import java.net.HttpURLConnection
import java.net.URL

// Updates installs a newer build of the spike over itself through
// PackageInstaller, asking Android to need no tap: an app may update
// itself so from Android 12 on, once the member allows it to install apps.
object Updates {
    fun install(ctx: Context, url: String) {
        Thread {
            try {
                val started = System.nanoTime()
                val c = URL(url).openConnection() as HttpURLConnection
                c.connectTimeout = 5000
                if (c.responseCode != 200) throw Exception("$url answered ${c.responseCode}")
                val pi = ctx.packageManager.packageInstaller
                val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
                params.setAppPackageName(ctx.packageName)
                params.setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
                if (Build.VERSION.SDK_INT >= 33) params.setPackageSource(PackageInstaller.PACKAGE_SOURCE_DOWNLOADED_FILE)
                val id = pi.createSession(params)
                pi.openSession(id).use { session ->
                    session.openWrite("base.apk", 0, c.contentLengthLong).use { out ->
                        c.inputStream.use { it.copyTo(out, 1 shl 16) }
                        session.fsync(out)
                    }
                    val done = PendingIntent.getBroadcast(
                        ctx, id, Intent(ctx, InstallReceiver::class.java),
                        PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
                    )
                    Spike.log(ctx, "update: session $id written in ${(System.nanoTime() - started) / 1_000_000} ms, committing")
                    session.commit(done.intentSender)
                }
            } catch (e: Exception) {
                Spike.log(ctx, "update: failed: $e")
            }
        }.start()
    }
}

class InstallReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, -999)
        val message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)
        Spike.log(ctx, "update: status $status ${message ?: ""}")
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
            // Android wants a tap after all: show its prompt.
            @Suppress("DEPRECATION")
            val confirm = (if (Build.VERSION.SDK_INT >= 33) intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
            else intent.getParcelableExtra(Intent.EXTRA_INTENT)) ?: return
            confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            ctx.startActivity(confirm)
        }
    }
}

// StartReceiver starts the service again after a reboot and after an
// update, which ends the app's processes, if it was running.
class StartReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val wanted = Spike.prefs(ctx).getBoolean("running", false)
        Spike.log(ctx, "start: ${intent.action}, service wanted: $wanted")
        if (!wanted) return
        try {
            DensService.start(ctx)
        } catch (e: Exception) {
            Spike.log(ctx, "start: refused: $e")
        }
    }
}
