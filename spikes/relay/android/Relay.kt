package chat.dens.shell

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.wifi.WifiManager
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.view.Gravity
import android.view.ViewGroup
import android.webkit.PermissionRequest
import android.webkit.WebChromeClient
import android.widget.FrameLayout
import java.io.File

// The Relay spike's part of the shell (spikes/relay): the probe run beside
// Dens, as the app, and the probe's page playing in a WebView of its own
// under the page, held as shown the way a call's page is. While it plays,
// the service has media playback's type, and the shell holds Wi-Fi awake.
//
// adb starts and stops it through the page's activity:
//   am start -n chat.dens.Dens.dev/chat.dens.shell.PageActivity --es relay "-peer 10.0.0.5 -role b"
//   am start -n chat.dens.Dens.dev/chat.dens.shell.PageActivity --es relay stop
// with --ez wake true to hold the processor awake too, --ez wifi false to
// leave Wi-Fi alone, and --es page QUERY for the player page's query.
object Relay {
    const val ACTION = "relay"

    @Volatile
    var on = false
        private set

    private var process: Process? = null
    private var wifi: WifiManager.WifiLock? = null
    private var wake: PowerManager.WakeLock? = null
    private var web: HeldWebView? = null
    private val main = Handler(Looper.getMainLooper())

    fun asked(activity: PageActivity, root: FrameLayout, intent: Intent?) {
        val args = intent?.getStringExtra("relay") ?: return
        stop(activity, root)
        if (args == "stop") return
        start(activity, root, args, intent.getBooleanExtra("wifi", true), intent.getBooleanExtra("wake", false),
            intent.getStringExtra("page") ?: "auto=1&name=phone")
    }

    private fun start(activity: PageActivity, root: FrameLayout, args: String, holdWifi: Boolean, holdWake: Boolean, query: String) {
        val ctx = activity.applicationContext
        val dir = File(ctx.filesDir, "shell")
        val command = listOf(File(ctx.applicationInfo.nativeLibraryDir, "librelayprobe.so").path) +
            args.split(' ').filter { it.isNotBlank() } +
            listOf("-quiet", "-page", "localhost:47880", "-log", File(dir, "relay.log").path)
        val pb = ProcessBuilder(command).directory(ctx.filesDir).redirectErrorStream(true)
            .redirectOutput(ProcessBuilder.Redirect.appendTo(File(dir, "relay.out")))
        pb.environment().putAll(Dens.env(ctx))
        process = pb.start()
        on = true
        Dens.log(ctx, "relay: the probe started: $args, wifi lock $holdWifi, wake lock $holdWake")
        if (holdWifi) {
            wifi = ctx.getSystemService(WifiManager::class.java).createWifiLock(WifiManager.WIFI_MODE_FULL_LOW_LATENCY, "dens:relay")
                .apply { acquire() }
        }
        if (holdWake) {
            wake = ctx.getSystemService(PowerManager::class.java).newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "dens:relay")
                .apply { acquire() }
        }
        // The service takes media playback's type.
        ctx.startForegroundService(Intent(ctx, DensService::class.java).setAction(ACTION))

        val w = HeldWebView(activity)
        w.settings.javaScriptEnabled = true
        w.settings.mediaPlaybackRequiresUserGesture = false
        // Question 9's page opens the microphone as a call's does: the
        // shell grants it as it grants the page's, the service taking the
        // microphone's type and routing the call.
        w.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                if (PermissionRequest.RESOURCE_AUDIO_CAPTURE in request.resources &&
                    ctx.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
                ) {
                    DensService.call(ctx)
                    request.grant(arrayOf(PermissionRequest.RESOURCE_AUDIO_CAPTURE))
                    Dens.log(ctx, "relay: granted its page the microphone")
                } else {
                    request.deny()
                }
            }
        }
        w.held = true
        root.addView(w, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 4, Gravity.BOTTOM))
        web = w
        // Once the probe has had a moment to serve it.
        main.postDelayed({ if (web === w) w.loadUrl("http://localhost:47880/?$query") }, 1500)
    }

    fun stop(ctx: Context, root: FrameLayout?) {
        if (!on) return
        web?.let {
            root?.removeView(it)
            it.destroy()
        }
        web = null
        process?.destroy()
        process = null
        wifi?.takeIf { it.isHeld }?.release()
        wifi = null
        wake?.takeIf { it.isHeld }?.release()
        wake = null
        on = false
        Dens.log(ctx, "relay: stopped")
        ctx.startForegroundService(Intent(ctx.applicationContext, DensService::class.java).setAction(ACTION))
    }
}
