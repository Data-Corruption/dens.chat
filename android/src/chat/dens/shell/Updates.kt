package chat.dens.shell

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.provider.Settings
import java.io.File
import java.io.FileInputStream

// Updates hands the updates Dens downloads to Android's installer (M12.4).
// Dens checks each one against the release's checksums and their signature
// before it says it's ready; the shell posts that it is, and its tap, or
// the page's Install, which Dens passes on, writes the APK into an install
// session that asks the member to confirm. Android takes it only signed
// with the key the app was installed with, at a higher version code, and
// once it has, starts the service again (MY_PACKAGE_REPLACED). The app asks
// for REQUEST_INSTALL_PACKAGES alone: Play Protect calls an app that may
// update itself without asking harmful.
object Updates {
    private const val CHANNEL = "updates"
    private const val TAG = "update"
    // What a tap on the notification asks of the page's activity, with the
    // update's version and APK in extras of these names.
    const val INSTALL = "chat.dens.shell.INSTALL"
    const val VERSION = "version"
    const val APK = "apk"
    // The install session's status, which Android sends UpdateReceiver.
    const val STATUS = "chat.dens.shell.UPDATE_STATUS"
    // Android 17's word on a developer it couldn't verify, on an install
    // it refused: PackageInstaller.EXTRA_DEVELOPER_VERIFICATION_FAILURE_REASON
    // and its reasons, which older Androids never send.
    private const val VERIFICATION = "android.content.pm.extra.DEVELOPER_VERIFICATION_FAILURE_REASON"
    private const val VERIFICATION_OFFLINE = 1
    private val apkName = Regex("^android-[a-z0-9]+\\.apk$")

    // apk is the APK Dens names, if it's one Dens may have kept: in the
    // instance's update directory, named as a release names its APKs.
    fun apk(ctx: Context, path: String?): File? {
        val f = File(path ?: return null)
        val dir = Dens.updateDir(ctx).canonicalFile
        return f.takeIf { it.parentFile?.canonicalFile == dir && apkName.matches(it.name) && it.isFile }
    }

    private fun manager(ctx: Context) = ctx.getSystemService(NotificationManager::class.java)

    // The Updates channel alerts, once an update, at default importance:
    // Android bundles an app's quiet notifications, the service's among
    // them, under one whose tap only opens the app.
    private fun channel(ctx: Context) {
        manager(ctx).createNotificationChannel(NotificationChannel(CHANNEL, "Updates", NotificationManager.IMPORTANCE_DEFAULT).apply {
            description = "When an update to Dens is ready to install"
            setShowBadge(false)
        })
    }

    // opens is the tap that installs version from apk.
    private fun opens(ctx: Context, version: String, apk: File): PendingIntent {
        val i = Intent(ctx, PageActivity::class.java).setAction(INSTALL).putExtra(VERSION, version).putExtra(APK, apk.path)
        return PendingIntent.getActivity(ctx, TAG.hashCode(), i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    // post shows the update's notification, saying text, its tap installing
    // it, with a button where there's somewhere to go first, in a group of
    // its own.
    private fun post(ctx: Context, version: String, apk: File, title: String, text: String, go: Notification.Action? = null) {
        channel(ctx)
        val b = Notification.Builder(ctx, CHANNEL)
            .setSmallIcon(Dens.drawable(ctx, "notification"))
            .setGroup(TAG)
            .setContentTitle(title)
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setOnlyAlertOnce(true)
            .setAutoCancel(true)
            .setContentIntent(opens(ctx, version, apk))
        if (go != null) b.addAction(go)
        manager(ctx).notify(TAG, 0, b.build())
    }

    // offer is Dens saying an update is ready, which the notification
    // offers to install.
    fun offer(ctx: Context, version: String, path: String?) {
        val apk = apk(ctx, path) ?: return Dens.log(ctx, "update: Dens named an APK outside its update directory")
        Dens.log(ctx, "update: $version is ready")
        post(ctx, version, apk, "Dens $version is ready to install", "Tap to install it. Android asks you to confirm.")
    }

    // forget takes the notification back, as Dens starts again: it says
    // again what's still ready.
    fun forget(ctx: Context) = manager(ctx).cancel(TAG, 0)

    // install writes apk into an install session and commits it, which has
    // Android ask the member to confirm. It reads tens of megabytes, so it
    // runs off the main thread.
    fun install(ctx: Context, version: String, path: String?) {
        val app = ctx.applicationContext
        val apk = apk(app, path) ?: return Dens.log(app, "update: no APK to install")
        manager(app).cancel(TAG, 0)
        Thread({
            val pi = app.packageManager.packageInstaller
            var id = -1
            try {
                val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
                    setAppPackageName(app.packageName)
                    setSize(apk.length())
                    setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_REQUIRED)
                }
                id = pi.createSession(params)
                pi.openSession(id).use { s ->
                    FileInputStream(apk).use { input ->
                        s.openWrite("base.apk", 0, apk.length()).use { out ->
                            input.copyTo(out)
                            s.fsync(out)
                        }
                    }
                    val status = Intent(app, UpdateReceiver::class.java).setAction(STATUS).putExtra(VERSION, version).putExtra(APK, apk.path)
                    s.commit(PendingIntent.getBroadcast(app, id, status, PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_UPDATE_CURRENT).intentSender)
                }
                Dens.log(app, "update: handed $version to Android's installer, which may install apps from Dens: ${app.packageManager.canRequestPackageInstalls()}")
            } catch (e: Exception) {
                Dens.log(app, "update: couldn't hand $version to Android's installer: ${e.javaClass.simpleName}")
                if (id >= 0) {
                    try {
                        pi.abandonSession(id)
                    } catch (e: Exception) {
                    }
                }
                post(app, version, apk, "Dens $version didn't install", "Android's installer couldn't take it. Tap to try again.")
            }
        }, "dens-update").start()
    }

    // status is Android's word on the install session.
    fun status(ctx: Context, intent: Intent) {
        val version = intent.getStringExtra(VERSION) ?: ""
        val apk = apk(ctx, intent.getStringExtra(APK))
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        when (status) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                @Suppress("DEPRECATION")
                val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT) ?: return
                confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                // Android starts an app's activity only while the app is in
                // front, as it is just after a tap; behind the others, the
                // notification asks for the tap that confirms it.
                if (DensService.isFront) {
                    Dens.log(ctx, "update: Android asks the member to confirm $version")
                    ctx.startActivity(confirm)
                } else if (apk != null) {
                    Dens.log(ctx, "update: Android asks the member to confirm $version, behind the other apps")
                    channel(ctx)
                    val tap = PendingIntent.getActivity(ctx, TAG.hashCode() + 1, confirm, PendingIntent.FLAG_IMMUTABLE)
                    manager(ctx).notify(TAG, 0, Notification.Builder(ctx, CHANNEL)
                        .setSmallIcon(Dens.drawable(ctx, "notification"))
                        .setGroup(TAG)
                        .setContentTitle("Dens $version is ready to install")
                        .setContentText("Tap to confirm it.")
                        .setAutoCancel(true)
                        .setContentIntent(tap)
                        .build())
                }
            }
            PackageInstaller.STATUS_SUCCESS -> Dens.log(ctx, "update: Android installed $version")
            PackageInstaller.STATUS_FAILURE_ABORTED -> {
                Dens.log(ctx, "update: the member didn't install $version")
                if (apk != null) post(ctx, version, apk, "Dens $version is ready to install", "Tap to install it. Android asks you to confirm.")
            }
            else -> {
                val reason = if (intent.hasExtra(VERIFICATION)) intent.getIntExtra(VERIFICATION, 0) else null
                Dens.log(ctx, "update: Android didn't install $version: status $status${if (reason != null) ", developer verification $reason" else ""}")
                if (apk == null) return
                when (reason) {
                    null -> post(ctx, version, apk, "Dens $version didn't install",
                        "Android said: ${intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE) ?: "it couldn't"}. Tap to try again.")
                    VERIFICATION_OFFLINE -> post(ctx, version, apk, "Dens $version didn't install",
                        "Android couldn't reach Google to check its developer. Tap to try again once you're online.")
                    else -> {
                        val dev = Intent(Settings.ACTION_APPLICATION_DEVELOPMENT_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        val options = PendingIntent.getActivity(ctx, TAG.hashCode() + 2, dev, PendingIntent.FLAG_IMMUTABLE)
                        post(ctx, version, apk, "Dens $version didn't install",
                            "Android refused it as from a developer it hasn't verified. In Developer options, allow apps from unverified developers, for good, then tap here to try again.",
                            Notification.Action.Builder(null, "Developer options", options).build())
                    }
                }
            }
        }
    }
}

// UpdateReceiver hears Android's word on an install session: Android alone
// sends it, through the session's own pending intent.
class UpdateReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        if (intent.action == Updates.STATUS) Updates.status(ctx, intent)
    }
}
