package chat.dens.shell

import android.content.Context
import android.net.LocalSocket
import android.net.LocalSocketAddress
import org.json.JSONObject
import java.io.BufferedReader
import java.io.Closeable
import java.io.IOException
import java.io.InputStreamReader

// Control talks to Dens over its control socket, in the instance's files,
// as the dens command does (internal/control): a request is a JSON line,
// and its reply a JSON line. On the shell's stream lines go both ways after
// that, each side pinging when it has nothing else to say, and giving the
// other up after StreamIdle without a line. Dens answers the app alone, by
// the UID the kernel names for the connection.
object Control {
    // The stream's pace, as internal/control's StreamPing and StreamIdle.
    private const val PING_MS = 15_000L
    private const val IDLE_MS = 45_000

    // Refused is Dens answering a request with an error.
    class Refused(message: String) : IOException(message)

    internal class Connection(val socket: LocalSocket, val reader: BufferedReader)

    private fun connect(ctx: Context, timeoutMs: Int): Connection {
        val socket = LocalSocket()
        try {
            socket.connect(LocalSocketAddress(Dens.socket(ctx).path, LocalSocketAddress.Namespace.FILESYSTEM))
            socket.soTimeout = timeoutMs
            return Connection(socket, BufferedReader(InputStreamReader(socket.inputStream, Charsets.UTF_8)))
        } catch (e: IOException) {
            socket.close()
            throw e
        }
    }

    private fun request(c: Connection, op: String): JSONObject {
        c.socket.outputStream.write((JSONObject().put("op", op).toString() + "\n").toByteArray())
        val line = c.reader.readLine() ?: throw IOException("Dens closed the connection")
        val reply = JSONObject(line)
        if (!reply.optBoolean("ok")) throw Refused(reply.optString("error", "refused"))
        return reply.optJSONObject("result") ?: JSONObject()
    }

    // call makes one request of Dens and returns its result.
    fun call(ctx: Context, op: String): JSONObject {
        val c = connect(ctx, 30_000)
        c.socket.use { return request(c, op) }
    }

    // open opens the shell's stream with Dens (OpShell).
    fun open(ctx: Context): Stream {
        val c = connect(ctx, IDLE_MS)
        try {
            request(c, "shell")
        } catch (e: Exception) {
            c.socket.close()
            throw e
        }
        return Stream(c)
    }

    // Stream is the shell's stream with Dens, which pings it while it's open.
    class Stream internal constructor(private val c: Connection) : Closeable {
        private val pinger = Thread {
            try {
                while (true) {
                    Thread.sleep(PING_MS)
                    send("ping")
                }
            } catch (e: InterruptedException) {
            } catch (e: IOException) {
                close()
            }
        }.apply {
            isDaemon = true
            start()
        }

        // next is the stream's next message but a ping, or null once the
        // stream has ended.
        fun next(): JSONObject? {
            while (true) {
                val line = try {
                    c.reader.readLine()
                } catch (e: IOException) {
                    null
                } ?: return null
                val msg = JSONObject(line)
                if (msg.optString("t") != "ping") return msg
            }
        }

        @Synchronized
        fun send(type: String) {
            c.socket.outputStream.write((JSONObject().put("t", type).toString() + "\n").toByteArray())
        }

        override fun close() {
            pinger.interrupt()
            try {
                c.socket.close()
            } catch (e: IOException) {
            }
        }
    }
}
