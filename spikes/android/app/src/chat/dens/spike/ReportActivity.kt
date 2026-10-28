package chat.dens.spike

import android.Manifest
import android.app.Activity
import android.app.ActivityManager
import android.app.Notification
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.usage.UsageStatsManager
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Typeface
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.system.Os
import android.system.OsConstants
import android.view.WindowInsets
import android.webkit.WebView
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import java.io.File

// ReportActivity is the spike's own screen: what the shell and the probe
// found, and a button for each test.
class ReportActivity : Activity() {
    private lateinit var status: TextView
    private lateinit var output: TextView
    private val main = Handler(Looper.getMainLooper())
    // The buttons by name, for adb: am start -n chat.dens.spike/.ReportActivity --es do run-the-report
    private val actions = mutableMapOf<String, () -> Unit>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val list = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        val scroll = ScrollView(this).apply { addView(list) }
        scroll.setOnApplyWindowInsetsListener { v, insets ->
            val i = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.displayCutout())
            v.setPadding(i.left + 24, i.top + 16, i.right + 24, i.bottom + 16)
            WindowInsets.CONSUMED
        }
        setContentView(scroll)

        list.addView(TextView(this).apply { text = "Dens Spike"; textSize = 24f })
        status = TextView(this).apply { typeface = Typeface.MONOSPACE; textSize = 11f }
        list.addView(status)

        fun row(vararg buttons: Pair<String, () -> Unit>) {
            val r = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
            for ((label, action) in buttons) {
                actions[label.lowercase().replace(Regex("[^a-z0-9]+"), "-").trim('-')] = action
                r.addView(Button(this).apply {
                    text = label
                    isAllCaps = false
                    setOnClickListener { action() }
                }, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
            }
            list.addView(r)
        }
        row("Open the page" to { page(null) }, "Pair again" to { pairAgain() }, "Feature page" to { featurePage() })
        row("Run the report" to { report() }, "Heartbeat gaps" to { probe("gaps") }, "Share" to { share() })
        row("Start service" to { startService() }, "Stop service" to { DensService.stop(this); prefs(false) }, "Type: ${fgsType()}" to { toggleType() })
        row("Start without FGS" to { prefs(false); DensService.stop(this); main.postDelayed({ DensService.start(this, DensService.BACKGROUND) }, 1500) },
            "Kill the app" to { android.os.Process.killProcess(android.os.Process.myPid()) },
            "Processes" to { show(processes()) })
        row("Burn 4 × 2 min" to { burn(4) }, "Burn 1 × 2 min" to { burn(1) }, "Burn results" to { show(Spike.tail(File(Spike.dir(this), "burn.txt"), 60)) })
        row("Notify in 15 s" to { notifySoon() }, "Unwrap in 20 s" to { unwrapSoon() }, "Logs" to { logs() })
        row("Earpiece" to { show(Audio.route(this, "earpiece")) }, "Speaker" to { show(Audio.route(this, "speaker")) }, "Bluetooth" to { show(Audio.route(this, "bluetooth")) })
        row("Clear route" to { show(Audio.route(this, "clear")) }, "Audio now" to { show(Audio.describe(this)) }, "Update" to { update() })
        row("Notifications" to { ask(Manifest.permission.POST_NOTIFICATIONS) }, "Microphone" to { ask(Manifest.permission.RECORD_AUDIO) }, "Nearby" to { ask(Manifest.permission.BLUETOOTH_CONNECT) })
        row("Local network" to { ask("android.permission.ACCESS_LOCAL_NETWORK") }, "Battery" to { battery() }, "Installs" to { installs() })

        output = TextView(this).apply {
            typeface = Typeface.MONOSPACE
            textSize = 10f
            setTextIsSelectable(true)
        }
        list.addView(output)
        DensService.channels(this)
        if (Spike.prefs(this).getBoolean("running", true)) startService()
        tick()
        command(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        command(intent)
    }

    private fun command(intent: Intent?) {
        // page opens the page at an address for the screen-off trials, with
        // their extras: PageActivity isn't exported, so adb can't start it.
        intent?.getStringExtra("page")?.let { url ->
            val lock = intent.getBooleanExtra(PageActivity.LOCK, false)
            setShowWhenLocked(lock)
            setTurnScreenOn(lock)
            val i = Intent(this, PageActivity::class.java).putExtra(PageActivity.URL, url).putExtra(PageActivity.LOCK, lock)
            if (intent.hasExtra(PageActivity.HOLD)) i.putExtra(PageActivity.HOLD, intent.getBooleanExtra(PageActivity.HOLD, true))
            Spike.log(this, "adb: page ${url.substringBefore('?')}")
            startActivity(i)
            return
        }
        val name = intent?.getStringExtra("do") ?: return
        val action = actions[name]
        Spike.log(this, "adb: $name${if (action == null) " (no such button: ${actions.keys.sorted().joinToString()})" else ""}")
        action?.invoke()
    }

    private fun prefs(running: Boolean) = Spike.prefs(this).edit().putBoolean("running", running).apply()

    private fun startService() {
        prefs(true)
        DensService.start(this)
    }

    private fun fgsType() = Spike.prefs(this).getString("fgs", "specialUse")!!

    private fun toggleType() {
        val next = if (fgsType() == "specialUse") "remoteMessaging" else "specialUse"
        Spike.prefs(this).edit().putString("fgs", next).apply()
        DensService.start(this)
        recreate()
    }

    private fun tick() {
        status.text = DensService.status
        main.postDelayed({ tick() }, 2000)
    }

    // show puts text on the screen, and in last.txt for adb.
    private fun show(text: String) {
        output.text = text
        try {
            File(Spike.dir(this), "last.txt").writeText(text)
        } catch (e: Exception) {
        }
    }

    private fun page(url: String?) {
        val i = Intent(this, PageActivity::class.java)
        if (url != null) i.putExtra(PageActivity.URL, url)
        startActivity(i)
    }

    private fun background(label: String, work: () -> String) {
        show("$label…")
        Thread {
            val text = try {
                work()
            } catch (e: Exception) {
                "$label failed: $e"
            }
            main.post { show(text) }
        }.start()
    }

    private fun pairAgain() = background("Pairing") {
        val (code, out) = Spike.run(Spike.dens(this, "open", "--print"), timeoutSeconds = 30)
        val link = out.lines().map { it.trim() }.firstOrNull { it.startsWith("http://localhost") }
        if (link != null) {
            main.post { page(link) }
            "Paired again"
        } else {
            "open --print exited $code:\n$out"
        }
    }

    private fun featurePage() {
        DensService.start(this, DensService.FEATURES)
        main.postDelayed({ page("http://localhost:${Spike.FEATURE_PORT}/") }, 1500)
    }

    private fun probe(vararg args: String) = background("aprobe ${args.joinToString(" ")}") {
        Spike.run(Spike.command(this, "libprobe.so", *args)).second
    }

    private fun burn(n: Int) {
        Thread { Spike.run(Spike.command(this, "libprobe.so", "burn", n.toString(), "120"), timeoutSeconds = 600) }.start()
        show("Burning: $n workers for 2 minutes; see Burn results")
    }

    // report puts the shell's view first, then the probe's, which gets the
    // unwrapped key on its standard input.
    private fun report() = background("Running the report") {
        val shell = StringBuilder()
        shell.append(shellReport())
        var key: ByteArray? = null
        try {
            shell.append(Keys.ensure(this)).append('\n')
            val (b, ms) = Keys.unwrap(this)
            key = b
            shell.append("unwrapped 32 bytes in $ms ms, sha256 ${Keys.prefix(b)}\n")
        } catch (e: Exception) {
            shell.append("Keystore: $e\n")
        }
        val args = mutableListOf("report", "--key-stdin")
        gateway()?.let { args += listOf("--lan", "$it:80") }
        val (code, out) = Spike.run(Spike.command(this, "libprobe.so", *args.toTypedArray()), input = key ?: ByteArray(0))
        key?.fill(0)
        val text = "$shell\n(probe exited $code)\n$out"
        File(Spike.dir(this), "full-report.txt").writeText(text)
        text
    }

    private fun shellReport(): String {
        val s = StringBuilder("== shell\n")
        s.append("Android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT}), ${Build.MANUFACTURER} ${Build.MODEL}, patch ${Build.VERSION.SECURITY_PATCH}\n")
        s.append("build ${Build.FINGERPRINT}\n")
        s.append("ABIs ${Build.SUPPORTED_ABIS.joinToString()}, page size ${Os.sysconf(OsConstants._SC_PAGESIZE)}\n")
        val wv = WebView.getCurrentWebViewPackage()
        s.append("WebView ${wv?.packageName} ${wv?.versionName}\n")
        s.append("native library dir ${applicationInfo.nativeLibraryDir}\n")
        val perms = listOf(
            Manifest.permission.RECORD_AUDIO, Manifest.permission.POST_NOTIFICATIONS,
            Manifest.permission.BLUETOOTH_CONNECT, "android.permission.ACCESS_LOCAL_NETWORK",
        ).joinToString { "${it.substringAfterLast('.')} ${if (checkSelfPermission(it) == PackageManager.PERMISSION_GRANTED) "yes" else "no"}" }
        s.append("permissions: $perms, installs ${packageManager.canRequestPackageInstalls()}\n")
        val pm = getSystemService(PowerManager::class.java)
        val usage = getSystemService(UsageStatsManager::class.java)
        val am = getSystemService(ActivityManager::class.java)
        s.append("battery: unrestricted ${pm.isIgnoringBatteryOptimizations(packageName)}, saver ${pm.isPowerSaveMode}, bucket ${usage.appStandbyBucket}, background restricted ${am.isBackgroundRestricted}\n")
        val me = ActivityManager.RunningAppProcessInfo().also { ActivityManager.getMyMemoryState(it) }
        s.append("importance ${me.importance}, memory class ${am.memoryClass} MB, low memory ${am.isLowRamDevice}\n")
        val cm = getSystemService(ConnectivityManager::class.java)
        val net = cm.activeNetwork
        val caps = net?.let { cm.getNetworkCapabilities(it) }
        val lp = net?.let { cm.getLinkProperties(it) }
        val transport = when {
            caps == null -> "none"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "Wi-Fi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "mobile data"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> "other"
        }
        s.append("network $transport, private DNS ${lp?.isPrivateDnsActive} ${lp?.privateDnsServerName ?: ""}, metered ${cm.isActiveNetworkMetered}\n")
        s.append(Audio.describe(this)).append('\n')
        s.append(DensService.status).append('\n')
        return s.toString()
    }

    // gateway is the local network's router, which the probe tries for
    // Android 17's local network permission.
    private fun gateway(): String? {
        val cm = getSystemService(ConnectivityManager::class.java)
        val lp = cm.activeNetwork?.let { cm.getLinkProperties(it) } ?: return null
        return lp.routes.firstOrNull { it.isDefaultRoute && it.gateway != null && it.gateway!!.address.size == 4 }?.gateway?.hostAddress
    }

    // processes lists the app's own processes, which are all /proc shows
    // an app, with what Android scored each for the low-memory killer.
    private fun processes(): String {
        val me = android.os.Process.myUid()
        val rows = File("/proc").listFiles { f -> f.name.all { it.isDigit() } }.orEmpty().mapNotNull { d ->
            try {
                val status = File(d, "status").readLines()
                val uid = status.first { it.startsWith("Uid:") }.split(Regex("\\s+"))[1].toInt()
                if (uid != me) return@mapNotNull null
                val name = File(d, "comm").readText().trim()
                val state = status.first { it.startsWith("State:") }.substringAfter(':').trim()
                "${d.name} $name oom ${File(d, "oom_score_adj").readText().trim()} $state"
            } catch (e: Exception) {
                null
            }
        }
        return "uid $me\n" + rows.joinToString("\n")
    }

    private fun share() {
        val f = File(Spike.dir(this), "full-report.txt")
        val text = if (f.exists()) f.readText() else output.text.toString()
        startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text), "Share the report"))
    }

    private fun logs() {
        val dir = Spike.dir(this)
        show("== shell.log\n${Spike.tail(File(dir, "shell.log"), 60)}\n\n== dens.out\n${Spike.tail(File(dir, "dens.out"), 40)}")
    }

    private fun notifySoon() {
        show("Posting a notification in 15 s: leave the app now")
        main.postDelayed({
            val open = PendingIntent.getActivity(
                this, 2, Intent(this, PageActivity::class.java).putExtra(PageActivity.URL, "http://localhost:${Spike.PAGE_PORT}/"),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
            getSystemService(NotificationManager::class.java).notify(
                2,
                Notification.Builder(this, "messages")
                    .setSmallIcon(android.R.drawable.stat_notify_chat)
                    .setContentTitle("A message in #general")
                    .setContentText("Tap to open the page")
                    .setContentIntent(open)
                    .setAutoCancel(true)
                    .build()
            )
            Spike.log(this, "notification posted")
        }, 15_000)
    }

    private fun unwrapSoon() {
        show("Unwrapping the key in 20 s: lock the phone now")
        main.postDelayed({
            val result = try {
                val (b, ms) = Keys.unwrap(this)
                val p = Keys.prefix(b)
                b.fill(0)
                "unwrapped in $ms ms, sha256 $p"
            } catch (e: Exception) {
                "unwrap failed: $e"
            }
            val locked = getSystemService(android.app.KeyguardManager::class.java).isDeviceLocked
            Spike.log(this, "key: $result, device locked $locked")
        }, 20_000)
    }

    private fun ask(permission: String) {
        requestPermissions(arrayOf(permission), 9)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, results: IntArray) {
        val granted = results.isNotEmpty() && results[0] == PackageManager.PERMISSION_GRANTED
        Spike.log(this, "permission ${permissions.firstOrNull()}: ${if (granted) "granted" else "refused"}")
        show("${permissions.firstOrNull()?.substringAfterLast('.')}: ${if (granted) "granted" else "refused"}")
    }

    @Suppress("BatteryLife")
    private fun battery() {
        startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
    }

    private fun installs() {
        startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName")))
    }

    private fun update() {
        if (!packageManager.canRequestPackageInstalls()) {
            show("Allow the spike to install apps first (Installs)")
            return
        }
        show("Updating from ${Spike.UPDATE_URL}; see Logs")
        Updates.install(this, Spike.UPDATE_URL)
    }
}
