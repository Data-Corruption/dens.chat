package chat.dens.spike

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.view.WindowInsets
import android.view.WindowManager
import android.webkit.ConsoleMessage
import android.webkit.CookieManager
import android.webkit.PermissionRequest
import android.webkit.RenderProcessGoneDetail
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import android.widget.Toast
import java.net.HttpURLConnection
import java.net.URL

// PageActivity shows Dens's page, from the service on loopback, in a
// WebView with no JavaScript bridge: the page talks to the service alone,
// as it does in a desktop browser, and the shell answers only what the
// WebView asks of the app.
class PageActivity : Activity() {
    companion object {
        const val URL = "url"
        // HOLD turns holding the page as shown during a call on or off,
        // for the screen-off trials; it stays as set.
        const val HOLD = "hold"
        // LOCK shows the page over the lock screen, so the trials can turn
        // the screen off and on without anyone unlocking the phone.
        const val LOCK = "lock"
        private const val FILES = 1
        private const val MIC = 2

        @Volatile private var current: PageActivity? = null

        // release stops holding the page once its call is over.
        fun release() {
            current?.let { a -> a.runOnUiThread { a.hold(false) } }
        }
    }

    private lateinit var root: FrameLayout
    private var web: WebView? = null
    private var pendingMic: PermissionRequest? = null
    private var files: ValueCallback<Array<Uri>>? = null
    private var fullScreen: View? = null
    private var fullScreenDone: WebChromeClient.CustomViewCallback? = null
    private var waived = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        current = this
        WebView.setWebContentsDebuggingEnabled(true)
        root = FrameLayout(this)
        setContentView(root)
        // Android 15 and later draw every app edge to edge: the shell keeps
        // the page clear of the bars, the cutout and the keyboard.
        root.setOnApplyWindowInsetsListener { _, insets ->
            val i = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.ime() or WindowInsets.Type.displayCutout())
            web?.let {
                val lp = it.layoutParams as ViewGroup.MarginLayoutParams
                lp.setMargins(i.left, i.top, i.right, i.bottom)
                it.layoutParams = lp
            }
            WindowInsets.CONSUMED
        }
        if (Build.VERSION.SDK_INT >= 33) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(android.window.OnBackInvokedDispatcher.PRIORITY_DEFAULT) { back() }
        }
        makeWebView()
        open(intent)
    }

    @Deprecated("Android 12 sends back here")
    override fun onBackPressed() = back()

    private fun back() {
        val w = web
        when {
            fullScreen != null -> fullScreenDone?.onCustomViewHidden()
            w != null && w.canGoBack() -> w.goBack()
            else -> finish()
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        open(intent)
    }

    private fun open(intent: Intent?) {
        if (intent?.hasExtra(HOLD) == true) {
            val on = intent.getBooleanExtra(HOLD, true)
            Spike.prefs(this).edit().putBoolean("hold", on).apply()
            if (!on) hold(false)
        }
        if (intent?.hasExtra(LOCK) == true) lock(intent.getBooleanExtra(LOCK, false))
        val url = intent?.getStringExtra(URL)
        if (url != null) {
            web?.loadUrl(url)
            return
        }
        if (web?.url != null) return
        // The service may still be starting: wait for its page, then pair
        // if this WebView has no session yet.
        Thread {
            val page = "http://localhost:${Spike.PAGE_PORT}/"
            var up = false
            for (i in 0 until 100) {
                try {
                    val c = URL("${page}api/status").openConnection() as HttpURLConnection
                    c.connectTimeout = 500
                    c.readTimeout = 2000
                    if (c.responseCode in 200..499) up = true
                    c.disconnect()
                } catch (e: Exception) {
                }
                if (up) break
                Thread.sleep(300)
            }
            val cookie = CookieManager.getInstance().getCookie(page)
            val target = if (cookie.isNullOrEmpty()) pairingLink() ?: page else page
            Spike.log(this, "page: service ${if (up) "up" else "not answering"}, ${if (cookie.isNullOrEmpty()) "pairing" else "session kept"}")
            runOnUiThread { web?.loadUrl(target) }
        }.start()
    }

    // pairingLink asks the running service for a one-time pairing link,
    // as dens open --print does on a desktop.
    private fun pairingLink(): String? {
        val (code, out) = Spike.run(Spike.dens(this, "open", "--print"), timeoutSeconds = 30)
        val link = out.lines().map { it.trim() }.firstOrNull { it.startsWith("http://localhost") }
        Spike.log(this, "page: open --print exited $code, ${if (link != null) "with a link" else "without a link: ${out.take(300)}"}")
        return link
    }

    // hold keeps the page running as if on the screen while a call is up.
    // Chromium freezes a hidden page a minute after it last made sound,
    // and a frozen page's call stops sending.
    fun hold(on: Boolean) {
        val w = web as? HeldWebView ?: return
        if (w.held == on) return
        w.held = on
        w.setRendererPriorityPolicy(WebView.RENDERER_PRIORITY_IMPORTANT, waived && !on)
        Spike.log(this, "page: ${if (on) "held as shown" else "no longer held"}")
    }

    private fun lock(on: Boolean) {
        setShowWhenLocked(on)
        setTurnScreenOn(on)
        if (on) window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON) else window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        Spike.log(this, "page: ${if (on) "shows over the lock screen" else "stays behind the lock screen"}")
    }

    private fun makeWebView() {
        val w = HeldWebView(this)
        web = w
        waived = w.rendererPriorityWaivedWhenNotVisible
        Spike.log(this, "page: renderer priority ${w.rendererRequestedPriority}, waived when not visible $waived")
        root.addView(w, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        root.requestApplyInsets()
        w.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            // A call's sound starts when someone else talks, not on a tap.
            mediaPlaybackRequiresUserGesture = false
            allowFileAccess = false
            allowContentAccess = false
            setSupportMultipleWindows(false)
            javaScriptCanOpenWindowsAutomatically = false
        }
        w.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                if (!request.isForMainFrame) return false
                val u = request.url
                if (u.scheme == "http" && (u.host == "localhost" || u.host == "127.0.0.1")) return false
                // Every other address is for the phone's browser.
                Spike.log(this@PageActivity, "page: link to ${u.scheme}://${u.host} goes to the browser")
                try {
                    startActivity(Intent(Intent.ACTION_VIEW, u).addCategory(Intent.CATEGORY_BROWSABLE))
                } catch (e: Exception) {
                    Toast.makeText(this@PageActivity, "No app opens that link", Toast.LENGTH_SHORT).show()
                }
                return true
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                Spike.log(this@PageActivity, "page: renderer gone, crashed ${detail.didCrash()}, priority at exit ${detail.rendererPriorityAtExit()}")
                root.removeView(view)
                view.destroy()
                web = null
                makeWebView()
                open(null)
                return true
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (request.isForMainFrame) Spike.log(this@PageActivity, "page: load error ${error.errorCode} ${error.description}")
            }
        }
        w.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                val origin = request.origin
                Spike.log(this@PageActivity, "page: ${origin.host}:${origin.port} asks for ${request.resources.joinToString()}")
                if (origin.host != "localhost" || !request.resources.contains(PermissionRequest.RESOURCE_AUDIO_CAPTURE)) {
                    request.deny()
                    return
                }
                pendingMic = request
                if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
                    grantMic()
                } else {
                    requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO), MIC)
                }
            }

            override fun onPermissionRequestCanceled(request: PermissionRequest) {
                Spike.log(this@PageActivity, "page: permission request cancelled")
                if (pendingMic == request) pendingMic = null
            }

            override fun onShowFileChooser(view: WebView, callback: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                files?.onReceiveValue(null)
                files = callback
                val intent = params.createIntent()
                if (params.mode == FileChooserParams.MODE_OPEN_MULTIPLE) intent.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
                Spike.log(this@PageActivity, "page: file chooser for ${params.acceptTypes.joinToString()} mode ${params.mode}")
                return try {
                    startActivityForResult(intent, FILES)
                    true
                } catch (e: Exception) {
                    files = null
                    false
                }
            }

            override fun onShowCustomView(view: View, callback: CustomViewCallback) {
                Spike.log(this@PageActivity, "page: full screen")
                fullScreen = view
                fullScreenDone = callback
                root.addView(view, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
                window.insetsController?.hide(WindowInsets.Type.systemBars())
            }

            override fun onHideCustomView() {
                Spike.log(this@PageActivity, "page: full screen ends")
                fullScreen?.let { root.removeView(it) }
                fullScreen = null
                fullScreenDone = null
                window.insetsController?.show(WindowInsets.Type.systemBars())
            }

            override fun onConsoleMessage(m: ConsoleMessage): Boolean {
                if (m.messageLevel() == ConsoleMessage.MessageLevel.ERROR || m.messageLevel() == ConsoleMessage.MessageLevel.WARNING) {
                    // The source's address may carry the pairing token in
                    // its fragment: the log gets the file's name alone.
                    val source = m.sourceId().substringBefore('#').substringBefore('?').substringAfterLast('/')
                    Spike.log(this@PageActivity, "console ${m.messageLevel()}: ${m.message().take(300)} ($source:${m.lineNumber()})")
                }
                return true
            }
        }
        w.setDownloadListener { url, _, disposition, mime, length ->
            Downloads.save(this, url, disposition, mime, length)
        }
    }

    // grantMic gives the page the microphone, once the shell's service has
    // the microphone type, which keeps it open with the screen off.
    private fun grantMic() {
        DensService.start(this, DensService.MIC_ON)
        pendingMic?.grant(arrayOf(PermissionRequest.RESOURCE_AUDIO_CAPTURE))
        pendingMic = null
        Spike.log(this, "page: microphone granted")
        if (Spike.prefs(this).getBoolean("hold", true)) hold(true)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, results: IntArray) {
        if (requestCode != MIC) return
        if (results.isNotEmpty() && results[0] == PackageManager.PERMISSION_GRANTED) {
            grantMic()
        } else {
            Spike.log(this, "page: microphone refused")
            pendingMic?.deny()
            pendingMic = null
        }
    }

    @Deprecated("the shell has no AndroidX")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (requestCode != FILES) return
        val callback = files ?: return
        files = null
        var picked: Array<Uri>? = null
        if (resultCode == RESULT_OK && data != null) {
            val clip = data.clipData
            picked = if (clip != null) Array(clip.itemCount) { clip.getItemAt(it).uri } else data.data?.let { arrayOf(it) }
        }
        Spike.log(this, "page: picked ${picked?.size ?: 0} files")
        callback.onReceiveValue(picked)
    }

    override fun onPause() {
        super.onPause()
        CookieManager.getInstance().flush()
    }

    override fun onDestroy() {
        if (current == this) current = null
        web?.destroy()
        super.onDestroy()
    }
}

// HeldWebView tells WebView its window shows while it's held, whatever
// the window does, and passes the window's own visibility on otherwise.
class HeldWebView(ctx: Context) : WebView(ctx) {
    private var shown = View.VISIBLE
    var held = false
        set(on) {
            field = on
            super.onWindowVisibilityChanged(if (on) View.VISIBLE else shown)
        }

    override fun onWindowVisibilityChanged(visibility: Int) {
        shown = visibility
        Spike.log(context, "page: window ${if (visibility == View.VISIBLE) "shows" else "hidden"}${if (held) ", held as shown" else ""}")
        super.onWindowVisibilityChanged(if (held) View.VISIBLE else visibility)
    }
}
