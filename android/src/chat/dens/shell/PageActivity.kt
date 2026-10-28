package chat.dens.shell

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.WindowInsets
import android.view.WindowInsetsController
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
import android.widget.Button
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import android.window.OnBackInvokedDispatcher
import java.net.HttpURLConnection
import java.net.URL

// PageActivity shows Dens's page, which the service serves on loopback, in
// a WebView with no JavaScript bridge (M12): the page talks to the service
// alone, as in a desktop browser, and the shell answers only what the
// WebView asks of the app. The WebView stays on the page: every other
// address opens in the phone's browser, but YouTube's player in its frame.
class PageActivity : Activity() {
    private companion object {
        const val FILES = 1
    }

    private lateinit var root: FrameLayout
    private var web: WebView? = null
    // What shows over the page, or instead of it: Dens starting, or why it
    // can't show the page.
    private var note: View? = null
    // The page's origin, once the WebView has it, and whether its last load
    // failed, as when Dens started again under it.
    private var origin: Uri? = null
    private var failed = false
    private var toldOld = false
    private var files: ValueCallback<Array<Uri>>? = null
    private var fullScreen: View? = null
    private var fullScreenDone: WebChromeClient.CustomViewCallback? = null
    private val watcher: () -> Unit = { runOnUiThread { if (!isDestroyed) show() } }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        root = FrameLayout(this)
        setContentView(root)
        // Android 15 and later draw every app edge to edge, and the older
        // ones do here too, so the shell keeps the page clear of the bars,
        // the cutout and the keyboard itself.
        window.setDecorFitsSystemWindows(false)
        root.setOnApplyWindowInsetsListener { _, insets ->
            inset(insets)
            WindowInsets.CONSUMED
        }
        if (Build.VERSION.SDK_INT >= 33) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(OnBackInvokedDispatcher.PRIORITY_DEFAULT) { back() }
        }
        // The browser checks' driver reaches a development build's page
        // over adb, as it reaches Chromium.
        if (Dens.debuggable(this)) WebView.setWebContentsDebuggingEnabled(true)
        DensService.watch(watcher)
        DensService.start(this)
        show()
    }

    // show puts up what the activity has to show: the page, once Dens has
    // said where it is, or what keeps it from showing.
    private fun show() {
        val webView = WebView.getCurrentWebViewPackage()
        val major = webView?.versionName?.substringBefore('.')?.toIntOrNull() ?: 0
        if (major < Dens.WEBVIEW_FLOOR) {
            if (!toldOld) Dens.log(this, "page: the WebView is $major, older than ${Dens.WEBVIEW_FLOOR}")
            toldOld = true
            val have = if (webView == null) "no Android System WebView" else "Android System WebView $major"
            return note("Dens needs Android System WebView ${Dens.WEBVIEW_FLOOR} or later, and this phone has $have. Update it, then open Dens again.", "Update it") {
                updateWebView(webView?.packageName)
            }
        }
        DensService.problem?.let { why ->
            return note(why, "Try again") { DensService.start(this) }
        }
        val page = DensService.page
        if (page == null) {
            // Dens starting again under a page that's showing: the page
            // reconnects by itself.
            if (web == null) note("Starting Dens…")
            return
        }
        note(null)
        val at = Uri.parse(page)
        if (web == null || failed || at != origin) {
            failed = false
            open(at)
        }
    }

    // open loads the page in the WebView: as it is, while the WebView's
    // session still opens it, or else at a pairing link Dens gives the
    // shell, as dens open gives a desktop's browser.
    private fun open(page: Uri) {
        val w = webView()
        origin = page
        Thread {
            val target = try {
                if (paired(page)) page.toString() else pairingLink()
            } catch (e: Exception) {
                Dens.log(this, "page: couldn't check the page's session: ${e.javaClass.simpleName}")
                page.toString()
            }
            runOnUiThread { if (!isDestroyed) w.loadUrl(target) }
        }.start()
    }

    // paired asks the page's API whether the WebView's session cookie still
    // opens it: none, or one Dens forgot, as when the member logged out,
    // needs pairing again.
    private fun paired(page: Uri): Boolean {
        val cookie = CookieManager.getInstance().getCookie(page.toString()) ?: return false
        val c = URL(page.buildUpon().path("/api/status").build().toString()).openConnection() as HttpURLConnection
        c.setRequestProperty("Cookie", cookie)
        c.connectTimeout = 5_000
        c.readTimeout = 10_000
        return try {
            c.responseCode != HttpURLConnection.HTTP_UNAUTHORIZED
        } finally {
            c.disconnect()
        }
    }

    private fun pairingLink(): String {
        val link = Control.call(this, "pair").getString("url")
        Dens.log(this, "page: pairing the page")
        return link
    }

    // repair pairs the page again, once it's shown, where its session no
    // longer opens it: the member opening the app again after logging out
    // does.
    private fun repair() {
        val page = origin ?: return
        val w = web ?: return
        Thread {
            try {
                if (!paired(page)) {
                    val link = pairingLink()
                    runOnUiThread { if (!isDestroyed) w.loadUrl(link) }
                }
            } catch (e: Exception) {
                Dens.log(this, "page: couldn't check the page's session: ${e.javaClass.simpleName}")
            }
        }.start()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        repair()
    }

    override fun onRestart() {
        super.onRestart()
        repair()
    }

    private fun onPage(u: Uri): Boolean {
        val o = origin ?: return false
        return u.scheme == o.scheme && u.host == o.host && u.port == o.port
    }

    private fun webView(): WebView {
        web?.let { return it }
        val w = WebView(this)
        web = w
        root.addView(w, 0, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        root.requestApplyInsets()
        w.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            // A video plays, and a call's sound starts, when someone else
            // talks, not on a tap.
            mediaPlaybackRequiresUserGesture = false
            allowFileAccess = false
            allowContentAccess = false
            setSupportMultipleWindows(false)
            javaScriptCanOpenWindowsAutomatically = false
            setGeolocationEnabled(false)
            safeBrowsingEnabled = false
        }
        w.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                if (!request.isForMainFrame || onPage(request.url)) return false
                outward(request.url)
                return true
            }

            // The WebView writes its cookies out lazily, and as the
            // activity pauses: a session the page just paired for is kept
            // even should the app be killed in its first seconds.
            override fun onPageFinished(view: WebView, url: String) {
                view.postDelayed({ CookieManager.getInstance().flush() }, 2_000)
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (!request.isForMainFrame) return
                Dens.log(this@PageActivity, "page: the page didn't load: ${error.errorCode}")
                failed = true
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                Dens.log(this@PageActivity, "page: the WebView's renderer went, crashed ${detail.didCrash()}")
                root.removeView(view)
                view.destroy()
                web = null
                origin = null
                show()
                return true
            }
        }
        w.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) {
                // The page asks for the microphone in a call, which the
                // shell grants from M12.2 on, and for nothing else.
                Dens.log(this@PageActivity, "page: refused it ${request.resources.joinToString { it.substringAfterLast('.') }}")
                request.deny()
            }

            override fun onShowFileChooser(view: WebView, callback: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                files?.onReceiveValue(null)
                files = callback
                val pick = params.createIntent()
                if (params.mode == FileChooserParams.MODE_OPEN_MULTIPLE) pick.putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
                return try {
                    @Suppress("DEPRECATION") startActivityForResult(pick, FILES)
                    true
                } catch (e: ActivityNotFoundException) {
                    files = null
                    false
                }
            }

            override fun onShowCustomView(view: View, callback: CustomViewCallback) {
                fullScreen?.let { root.removeView(it) }
                fullScreen = view
                fullScreenDone = callback
                root.addView(view, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
                window.insetsController?.apply {
                    systemBarsBehavior = WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
                    hide(WindowInsets.Type.systemBars())
                }
            }

            override fun onHideCustomView() {
                fullScreen?.let { root.removeView(it) }
                fullScreen = null
                fullScreenDone = null
                window.insetsController?.show(WindowInsets.Type.systemBars())
            }

            override fun onConsoleMessage(m: ConsoleMessage): Boolean {
                if (m.messageLevel() == ConsoleMessage.MessageLevel.ERROR || m.messageLevel() == ConsoleMessage.MessageLevel.WARNING) {
                    // A message may quote what a member wrote, so only where
                    // it comes from goes in the log, by the file's name: the
                    // page's address carries its token while it pairs.
                    val source = m.sourceId().substringBefore('#').substringBefore('?').substringAfterLast('/')
                    Dens.log(this@PageActivity, "page: a console ${m.messageLevel().name.lowercase()} from $source:${m.lineNumber()}")
                }
                return true
            }
        }
        w.setDownloadListener { url, _, disposition, _, _ -> Downloads.save(this, origin, url, disposition) }
        return w
    }

    // outward opens an address that isn't the page's in the app that takes
    // it, the phone's browser for most.
    private fun outward(u: Uri) {
        Dens.log(this, "page: a ${u.scheme} link went out")
        try {
            startActivity(Intent(Intent.ACTION_VIEW, u).addCategory(Intent.CATEGORY_BROWSABLE))
        } catch (e: ActivityNotFoundException) {
            Toast.makeText(this, "No app on this phone opens that link", Toast.LENGTH_SHORT).show()
        }
    }

    private fun updateWebView(pkg: String?) {
        val name = pkg ?: "com.google.android.webview"
        try {
            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("market://details?id=$name")))
        } catch (e: ActivityNotFoundException) {
            startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$name")))
        }
    }

    // note shows text over the page, with a button where there's something
    // to do about it, or with text null, takes it away.
    private fun note(text: String?, action: String? = null, run: (() -> Unit)? = null) {
        note?.let { root.removeView(it) }
        note = null
        if (text == null) return
        val background = TypedValue().also { theme.resolveAttribute(android.R.attr.colorBackground, it, true) }
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setBackgroundColor(background.data)
            val pad = (32 * resources.displayMetrics.density).toInt()
            setPadding(pad, pad, pad, pad)
            isClickable = true
        }
        box.addView(TextView(this).apply {
            this.text = text
            textSize = 16f
            gravity = Gravity.CENTER
            setTextIsSelectable(true)
        })
        if (action != null && run != null) {
            box.addView(Button(this).apply {
                this.text = action
                isAllCaps = false
                setOnClickListener { run() }
            }, LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply {
                topMargin = (16 * resources.displayMetrics.density).toInt()
            })
        }
        root.addView(box, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        note = box
    }

    private fun inset(insets: WindowInsets) {
        val i = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.ime() or WindowInsets.Type.displayCutout())
        web?.let {
            val lp = it.layoutParams as ViewGroup.MarginLayoutParams
            lp.setMargins(i.left, i.top, i.right, i.bottom)
            it.layoutParams = lp
        }
    }

    // back walks the page's history, then leaves the page, as in a
    // browser; full screen goes first.
    private fun back() {
        val w = web
        when {
            fullScreen != null -> fullScreenDone?.onCustomViewHidden()
            w != null && w.canGoBack() -> w.goBack()
            else -> finish()
        }
    }

    @Deprecated("Android 12 sends Back here, and later ones to the callback")
    override fun onBackPressed() = back()

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
        callback.onReceiveValue(picked)
    }

    override fun onPause() {
        super.onPause()
        CookieManager.getInstance().flush()
    }

    override fun onDestroy() {
        DensService.unwatch(watcher)
        web?.destroy()
        web = null
        super.onDestroy()
    }
}
