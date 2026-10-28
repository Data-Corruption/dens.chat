package chat.dens.spike

import android.content.Context
import android.content.SharedPreferences
import android.system.Os
import android.util.Log
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import java.util.concurrent.TimeUnit

// What every part of the shell shares: where things live, the children's
// environment, and the log.
object Spike {
    const val TAG = "DensSpike"
    const val PAGE_PORT = 18484
    const val FEATURE_PORT = 18490
    // build.sh serves updates here, through adb reverse.
    const val UPDATE_URL = "http://127.0.0.1:18499/dens-spike.apk"
    const val INSTANCE = "main"

    private val stamp = SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS", Locale.US)

    fun prefs(ctx: Context): SharedPreferences = ctx.getSharedPreferences("spike", Context.MODE_PRIVATE)

    // The spike's directory, under the app's files: the children's working
    // directory and home, where the probe writes its reports.
    fun dir(ctx: Context): File = private(File(ctx.filesDir, "spike"))

    // Dens runs as a development instance, whose data goes under
    // $XDG_DATA_HOME/dens-dev and its control socket under
    // $XDG_RUNTIME_DIR/dens-dev.
    fun dataHome(ctx: Context): File = private(File(ctx.filesDir, "data"))
    fun runtimeDir(ctx: Context): File = private(File(ctx.filesDir, "run"))
    fun tmpDir(ctx: Context): File = private(File(ctx.cacheDir, "tmp"))

    private fun private(dir: File): File {
        if (!dir.isDirectory) dir.mkdirs()
        Os.chmod(dir.path, "700".toInt(8))
        return dir
    }

    fun lib(ctx: Context, name: String): String = ctx.applicationInfo.nativeLibraryDir + "/" + name

    // env is the children's environment. files/spike/env adds to it, a
    // KEY=VALUE a line, which adb writes on the emulator: SSL_CERT_FILE for
    // its den's test certificate authority.
    fun env(ctx: Context): Map<String, String> = base(ctx) + extra(ctx)

    private fun extra(ctx: Context): Map<String, String> {
        val f = File(dir(ctx), "env")
        if (!f.exists()) return emptyMap()
        return f.readLines().mapNotNull { l -> l.split('=', limit = 2).takeIf { it.size == 2 }?.let { it[0].trim() to it[1].trim() } }.toMap()
    }

    private fun base(ctx: Context): Map<String, String> = mapOf(
        "HOME" to dir(ctx).path,
        "TMPDIR" to tmpDir(ctx).path,
        "XDG_DATA_HOME" to dataHome(ctx).path,
        "XDG_RUNTIME_DIR" to runtimeDir(ctx).path,
        "XDG_CONFIG_HOME" to File(dir(ctx), "config").path,
        "XDG_CACHE_HOME" to File(ctx.cacheDir, "xdg").path,
        // Go on Android takes the local zone from TZ alone.
        "TZ" to TimeZone.getDefault().id,
    )

    // command builds a child process: one of the binaries in the native
    // library directory, in the spike's directory, with the environment.
    fun command(ctx: Context, binary: String, vararg args: String): ProcessBuilder {
        val pb = ProcessBuilder(listOf(lib(ctx, binary)) + args)
        pb.directory(dir(ctx))
        pb.environment().putAll(env(ctx))
        return pb
    }

    fun dens(ctx: Context, vararg args: String): ProcessBuilder = command(ctx, "libdens.so", "-i", INSTANCE, *args)

    // run runs a child to its end and returns its exit status and output.
    fun run(pb: ProcessBuilder, input: ByteArray? = null, timeoutSeconds: Long = 120): Pair<Int, String> {
        pb.redirectErrorStream(true)
        val p = pb.start()
        if (input != null) {
            p.outputStream.use { it.write(input) }
        } else {
            p.outputStream.close()
        }
        val out = StringBuilder()
        val reader = Thread { out.append(p.inputStream.bufferedReader().readText()) }
        reader.start()
        if (!p.waitFor(timeoutSeconds, TimeUnit.SECONDS)) {
            p.destroyForcibly()
            reader.join(2000)
            return Pair(-1, out.toString() + "\n(timed out after $timeoutSeconds s)")
        }
        reader.join(5000)
        return Pair(p.exitValue(), out.toString())
    }

    @Synchronized
    fun log(ctx: Context, msg: String) {
        Log.i(TAG, msg)
        try {
            File(dir(ctx), "shell.log").appendText("${stamp.format(Date())} $msg\n")
        } catch (e: Exception) {
            Log.w(TAG, "log: $e")
        }
    }

    fun tail(file: File, lines: Int): String {
        if (!file.exists()) return "(no ${file.name})"
        val all = file.readLines()
        return all.takeLast(lines).joinToString("\n")
    }
}
