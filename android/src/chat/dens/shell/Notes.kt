package chat.dens.shell

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import org.json.JSONObject

// Notes posts the member's notifications (M12.3): each one Dens gives the
// shell, in a Messages channel of their own, grouped by den, and a tap
// opens the page at the message. Dens decides what notifies the member,
// and takes a notification back as they read its channel, wherever they
// read it. A channel's newer notification replaces its last, under the
// channel's key.
object Notes {
    private const val CHANNEL = "messages"
    // A den's summary, which groups its notifications, goes under its ID
    // after this.
    private const val SUMMARY = "den:"
    // What a tap on one asks of the page's activity: to open the page at
    // the path in PATH.
    const val OPEN = "chat.dens.shell.OPEN"
    const val PATH = "path"
    // The paths a tap opens: a den, or a channel's message there.
    private val path = Regex("^/den/[A-Za-z0-9_-]+(/[0-9]+#message=[0-9]+)?$")

    private fun manager(ctx: Context) = ctx.getSystemService(NotificationManager::class.java)

    // allowed is whether Android lets the app post the member's
    // notifications: they allowed notifications, and didn't turn off the
    // Messages channel.
    fun allowed(ctx: Context): Boolean {
        val nm = manager(ctx)
        return nm.areNotificationsEnabled() && nm.getNotificationChannel(CHANNEL)?.importance != NotificationManager.IMPORTANCE_NONE
    }

    // openable is a path a tap may open, or null.
    fun openable(p: String?): String? = p?.takeIf { path.matches(it) }

    private fun opens(ctx: Context, code: Int, p: String): PendingIntent {
        val i = Intent(ctx, PageActivity::class.java).setAction(OPEN).putExtra(PATH, p)
        return PendingIntent.getActivity(ctx, code, i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    // post posts a notification Dens gave: its title and text, its den's
    // name above them, under its den's summary.
    fun post(ctx: Context, n: JSONObject) {
        val key = n.optString("key")
        val den = n.optString("den")
        val p = openable(n.optString("path"))
        if (key.isEmpty() || den.isEmpty() || p == null) return
        val nm = manager(ctx)
        nm.createNotificationChannel(NotificationChannel(CHANNEL, "Messages", NotificationManager.IMPORTANCE_HIGH).apply {
            description = "Direct messages and mentions, as you turn them on in Dens's settings"
        })
        if (!allowed(ctx)) {
            Dens.log(ctx, "notes: Android doesn't let the app post notifications")
            return
        }
        val icon = Dens.drawable(ctx, "notification")
        val text = n.optString("text")
        val note = Notification.Builder(ctx, CHANNEL)
            .setSmallIcon(icon)
            .setContentTitle(n.optString("title"))
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setSubText(n.optString("denName"))
            .setCategory(Notification.CATEGORY_MESSAGE)
            .setGroup(den)
            .setShowWhen(true)
            .setAutoCancel(true)
            .setContentIntent(opens(ctx, key.hashCode(), p))
            .build()
        nm.notify(key, 0, note)
        val summary = Notification.Builder(ctx, CHANNEL)
            .setSmallIcon(icon)
            .setSubText(n.optString("denName"))
            .setCategory(Notification.CATEGORY_MESSAGE)
            .setGroup(den)
            .setGroupSummary(true)
            .setGroupAlertBehavior(Notification.GROUP_ALERT_CHILDREN)
            .setAutoCancel(true)
            .setContentIntent(opens(ctx, (SUMMARY + den).hashCode(), "/den/$den"))
            .build()
        nm.notify(SUMMARY + den, 0, summary)
        Dens.log(ctx, "notes: posted a notification")
    }

    // cancel takes back the notifications of keys, and a den's summary
    // once none of its notifications is left.
    fun cancel(ctx: Context, keys: List<String>) {
        val nm = manager(ctx)
        val dens = mutableSetOf<String>()
        for (key in keys) {
            nm.cancel(key, 0)
            dens += key.substringBefore('/')
        }
        // Android may list what was just cancelled a moment longer.
        val left = nm.activeNotifications.mapNotNull { it.tag }.filter { it !in keys }
        for (den in dens) {
            if (left.none { it.startsWith("$den/") }) nm.cancel(SUMMARY + den, 0)
        }
        Dens.log(ctx, "notes: took back ${keys.size}")
    }
}
