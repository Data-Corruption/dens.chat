package chat.dens.shell

import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Build
import android.provider.Settings
import android.system.Os
import android.util.Log
import java.io.File
import java.io.IOException
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

// What the shell's parts share: where Dens keeps its instance, the
// environment the service runs in, and the shell's own log.
object Dens {
    private const val TAG = "Dens"
    // The instance the app runs: Android's Dens has one.
    private const val INSTANCE = "main"
    // The oldest WebView the page works in. Tailwind's styles need Chromium
    // 111, which knows the CSP's 'wasm-unsafe-eval' as well, which RNNoise
    // needs and Android 12's own WebView, 91, doesn't.
    const val WEBVIEW_FLOOR = 111
    // Dens's exit status for a start that mustn't be tried again by itself,
    // as when its data or its key is wrong (host.ExitRefused).
    const val EXIT_REFUSED = 78
    private const val LOG_LIMIT = 256 * 1024L

    fun debuggable(ctx: Context) = (ctx.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) != 0

    // The instance's paths, as internal/layout puts them for the Android
    // app, under the files directory the shell names in XDG_DATA_HOME.
    private fun root(ctx: Context) = File(ctx.filesDir, "dens/$INSTANCE")
    private fun control(ctx: Context) = File(root(ctx), "control")
    fun wrappedKey(ctx: Context) = File(control(ctx), "datakey.keystore")
    fun state(ctx: Context) = File(control(ctx), "state.json")
    fun socket(ctx: Context) = File(root(ctx), "run/control.sock")
    // Where Dens keeps an update it downloaded and checked (M12.4).
    fun updateDir(ctx: Context) = File(root(ctx), "update")

    // controlDir makes the instance's control directory as the service
    // wants it, private to the app, for the key the shell wraps before the
    // service first starts.
    fun controlDir(ctx: Context): File {
        private(File(ctx.filesDir, "dens"))
        private(root(ctx))
        return private(control(ctx))
    }

    // The shell's own files: its log, and the service's output, which says
    // why a start failed before Dens's own log opened.
    private fun shellDir(ctx: Context) = private(File(ctx.filesDir, "shell"))
    private fun output(ctx: Context) = File(shellDir(ctx), "dens.out")

    // private makes a directory only the app may enter. One already there
    // keeps its mode, which the service checks rather than the shell repair.
    private fun private(dir: File): File {
        if (!dir.isDirectory) {
            if (!dir.mkdir() && !dir.isDirectory) throw IOException("couldn't make $dir")
            Os.chmod(dir.path, "700".toInt(8))
        }
        return dir
    }

    fun binary(ctx: Context) = File(ctx.applicationInfo.nativeLibraryDir, "libdens.so").path

    // env is what the service runs with beyond the app's own environment:
    // the app's files as its home and its data, where Go on Android would
    // otherwise take /sdcard for home; the cache for temporary files; the
    // phone's time zone, which Go reads from TZ alone there; and the
    // phone's name, as its owner knows it from Android's settings, and its
    // model, which dens list it by among the member's devices (M13.3),
    // where its hostname says only localhost.
    fun env(ctx: Context): Map<String, String> = mapOf(
        "HOME" to ctx.filesDir.path,
        "XDG_DATA_HOME" to ctx.filesDir.path,
        "TMPDIR" to private(File(ctx.cacheDir, "tmp")).path,
        "TZ" to TimeZone.getDefault().id,
        "DENS_DEVICE_NAME" to (Settings.Global.getString(ctx.contentResolver, Settings.Global.DEVICE_NAME) ?: ""),
        "DENS_DEVICE_MODEL" to Build.MODEL,
    ) + developmentEnv(ctx)

    // A development build takes more from shell/env, a KEY=VALUE a line,
    // which adb writes through run-as: SSL_CERT_FILE for the certificate
    // authority of the emulator's den.
    private fun developmentEnv(ctx: Context): Map<String, String> {
        val f = File(shellDir(ctx), "env")
        if (!debuggable(ctx) || !f.isFile) return emptyMap()
        return f.readLines().mapNotNull { line ->
            line.split('=', limit = 2).takeIf { it.size == 2 && it[0].isNotBlank() }?.let { it[0].trim() to it[1].trim() }
        }.toMap()
    }

    // The line the shell puts in the service's output before each start.
    private const val START = "== Dens starts at "

    // freshOutput is the file the service's output goes to, kept to its
    // last few starts, each after a line of the shell's.
    @Synchronized
    fun freshOutput(ctx: Context): File {
        val f = output(ctx)
        if (f.length() > LOG_LIMIT) f.renameTo(File(shellDir(ctx), "dens.out.1"))
        f.appendText("$START${stamp.format(Date())}\n")
        return f
    }

    // lastWords is the end of what the service said as it last ran, which
    // says why it stopped.
    fun lastWords(ctx: Context): String = try {
        output(ctx).readLines().takeLastWhile { !it.startsWith(START) }.filter { it.isNotBlank() }.takeLast(5).joinToString("\n")
    } catch (e: IOException) {
        ""
    }

    private val stamp = SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS", Locale.US)

    // log notes what the shell did, in Android's log and the shell's own:
    // events, counts and states, never a message, a name, an address or a
    // token.
    @Synchronized
    fun log(ctx: Context, msg: String) {
        Log.i(TAG, msg)
        try {
            val f = File(shellDir(ctx), "shell.log")
            if (f.length() > LOG_LIMIT) f.renameTo(File(shellDir(ctx), "shell.log.1"))
            f.appendText("${stamp.format(Date())} $msg\n")
        } catch (e: IOException) {
            Log.w(TAG, "the shell's log: $e")
        }
    }

    // drawable is one of the app's own drawables. The code names no
    // resource, so the build makes no R class.
    fun drawable(ctx: Context, name: String): Int {
        val pkg = ctx.resources.getResourcePackageName(ctx.applicationInfo.icon)
        return ctx.resources.getIdentifier(name, "drawable", pkg)
    }
}
