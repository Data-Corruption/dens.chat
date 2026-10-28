package chat.dens.shell

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.provider.MediaStore
import android.webkit.CookieManager
import android.webkit.MimeTypeMap
import android.widget.Toast
import java.io.FileOutputStream
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLDecoder
import java.util.Locale

// Downloads saves what the page offers as a download to the phone's
// Downloads (M12). The WebView hands the shell a link, not the file, so the
// shell fetches it from the service with the page's session cookie, under
// the name the service gives it in its Content-Disposition: it sends every
// download as application/octet-stream, which leaves Android's own guess at
// a name ending in .bin.
object Downloads {
    private val main = Handler(Looper.getMainLooper())

    fun save(ctx: Context, page: Uri?, url: String, disposition: String?) {
        val u = Uri.parse(url)
        // The page's own files alone: a blob: or data: link lives in the
        // page, where the shell can't reach it.
        if (page == null || u.scheme != page.scheme || u.host != page.host || u.port != page.port) {
            Dens.log(ctx, "download: refused a ${u.scheme} link that isn't the page's")
            toast(ctx, "Dens can save only its own files")
            return
        }
        Thread {
            var name = "the file"
            val c = URL(url).openConnection() as HttpURLConnection
            try {
                CookieManager.getInstance().getCookie(url)?.let { c.setRequestProperty("Cookie", it) }
                c.connectTimeout = 5_000
                c.readTimeout = 60_000
                if (c.responseCode != HttpURLConnection.HTTP_OK) throw IOException("Dens answered ${c.responseCode}")
                name = fileName(c.getHeaderField("Content-Disposition") ?: disposition) ?: "download"
                val bytes = keep(ctx, name) { out -> c.inputStream.use { it.copyTo(out, 1 shl 16) } }
                // A file's name is a member's content: the log gets its size.
                Dens.log(ctx, "download: saved $bytes bytes")
                toast(ctx, "Saved $name to Downloads")
            } catch (e: Exception) {
                Dens.log(ctx, "download: failed: ${e.javaClass.simpleName}")
                toast(ctx, "Couldn't save $name: ${e.message}")
            } finally {
                c.disconnect()
            }
        }.start()
    }

    // keep writes a file to Downloads as name, with what write puts out, and
    // returns what write returns. The file shows in Downloads once it's on
    // the phone's storage whole, which is when the toast says it's saved.
    private fun keep(ctx: Context, name: String, write: (FileOutputStream) -> Long): Long {
        val extension = name.substringAfterLast('.', "").lowercase(Locale.ROOT)
        val values = ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, name)
            put(MediaStore.Downloads.MIME_TYPE, MimeTypeMap.getSingleton().getMimeTypeFromExtension(extension) ?: "application/octet-stream")
            put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS)
            put(MediaStore.Downloads.IS_PENDING, 1)
        }
        val resolver = ctx.contentResolver
        val item = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values)
            ?: throw IOException("Android made no file in Downloads")
        try {
            val fd = resolver.openFileDescriptor(item, "w") ?: throw IOException("Android didn't open the file")
            val bytes = ParcelFileDescriptor.AutoCloseOutputStream(fd).use { out ->
                write(out).also { out.fd.sync() }
            }
            values.clear()
            values.put(MediaStore.Downloads.IS_PENDING, 0)
            resolver.update(item, values, null, null)
            return bytes
        } catch (e: Exception) {
            resolver.delete(item, null, null)
            throw e
        }
    }

    // fileName is the name a Content-Disposition gives: the RFC 2231
    // filename*, which holds any name, before the plain filename, kept to
    // a name alone, with no folder in it.
    fun fileName(disposition: String?): String? {
        if (disposition == null) return null
        var plain: String? = null
        var extended: String? = null
        for (part in disposition.split(';').drop(1)) {
            val (key, value) = part.split('=', limit = 2).takeIf { it.size == 2 }?.let { it[0].trim().lowercase(Locale.ROOT) to it[1].trim() } ?: continue
            when (key) {
                "filename*" -> {
                    // charset'language'percent-encoded
                    val bits = value.split('\'', limit = 3)
                    if (bits.size == 3 && bits[0].equals("utf-8", ignoreCase = true)) {
                        extended = try {
                            URLDecoder.decode(bits[2].replace("+", "%2B"), "UTF-8")
                        } catch (e: IllegalArgumentException) {
                            null
                        }
                    }
                }
                "filename" -> plain = value.removeSurrounding("\"").replace("\\\"", "\"").replace("\\\\", "\\")
            }
        }
        val name = (extended ?: plain)
            ?.substringAfterLast('/')?.substringAfterLast('\\')
            ?.filter { it >= ' ' && it != '\u007f' }
            ?.trim()?.trimStart('.')
            ?.take(200)
        return name?.takeIf { it.isNotEmpty() }
    }

    private fun toast(ctx: Context, text: String) {
        main.post { Toast.makeText(ctx.applicationContext, text, Toast.LENGTH_SHORT).show() }
    }
}
