package chat.dens.spike

import android.app.Notification
import android.app.NotificationManager
import android.content.ContentValues
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.provider.MediaStore
import android.webkit.CookieManager
import android.webkit.MimeTypeMap
import android.webkit.URLUtil
import android.widget.Toast
import java.net.HttpURLConnection
import java.net.URL

// Downloads saves what the page offers as a download into the phone's
// Downloads, fetching it from the service with the page's session cookie:
// the WebView hands the app a link, not the file.
object Downloads {
    fun save(ctx: Context, url: String, disposition: String?, mime: String?, length: Long) {
        val main = Handler(Looper.getMainLooper())
        val u = android.net.Uri.parse(url)
        if (u.scheme != "http" || u.host != "localhost") {
            // A blob: or data: link lives in the page alone.
            Spike.log(ctx, "download: a ${u.scheme} link, which the shell can't fetch")
            main.post { Toast.makeText(ctx, "Can't save a ${u.scheme} link", Toast.LENGTH_LONG).show() }
            return
        }
        // A file's name is a member's content: the log gets its type and size.
        Spike.log(ctx, "download: ${mime ?: "?"}, $length bytes")
        Thread {
            val started = System.nanoTime()
            try {
                val c = URL(url).openConnection() as HttpURLConnection
                CookieManager.getInstance().getCookie(url)?.let { c.setRequestProperty("Cookie", it) }
                c.connectTimeout = 5000
                c.readTimeout = 30000
                if (c.responseCode != 200) throw Exception("the service answered ${c.responseCode}")
                // The service names the file in its Content-Disposition and
                // sends every download as application/octet-stream, so the
                // name is guessed with no type, which would swap the
                // extension for .bin, and the type comes from the extension.
                val name = URLUtil.guessFileName(url, c.getHeaderField("Content-Disposition") ?: disposition, null)
                val type = MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substringAfterLast('.', "").lowercase())
                val values = ContentValues().apply {
                    put(MediaStore.Downloads.DISPLAY_NAME, name)
                    put(MediaStore.Downloads.MIME_TYPE, type ?: "application/octet-stream")
                    put(MediaStore.Downloads.RELATIVE_PATH, "Download/Dens Spike")
                    put(MediaStore.Downloads.IS_PENDING, 1)
                }
                val resolver = ctx.contentResolver
                val item = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values) ?: throw Exception("MediaStore refused it")
                var bytes = 0L
                resolver.openOutputStream(item)!!.use { out ->
                    c.inputStream.use { bytes = it.copyTo(out, 1 shl 16) }
                }
                values.clear()
                values.put(MediaStore.Downloads.IS_PENDING, 0)
                resolver.update(item, values, null, null)
                val ms = (System.nanoTime() - started) / 1_000_000
                Spike.log(ctx, "download: saved $bytes bytes in $ms ms")
                ctx.getSystemService(NotificationManager::class.java).notify(
                    name.hashCode(),
                    Notification.Builder(ctx, "downloads")
                        .setSmallIcon(android.R.drawable.stat_sys_download_done)
                        .setContentTitle("Saved $name")
                        .setContentText("$bytes bytes in Downloads/Dens Spike")
                        .build()
                )
                main.post { Toast.makeText(ctx, "Saved $name", Toast.LENGTH_SHORT).show() }
            } catch (e: Exception) {
                Spike.log(ctx, "download: failed: $e")
                main.post { Toast.makeText(ctx, "Download failed: ${e.message}", Toast.LENGTH_LONG).show() }
            }
        }.start()
    }
}
