package chat.dens.shell

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build

// Local is Android's word on the app reaching its own network, which Relay
// links across (M20.3): Android 17 keeps an app off it unless the member
// allows it, and earlier Androids ask nothing. The shell asks once, as the
// member first wants Relay; after a refusal, Android's settings allow it.
object Local {
    const val PERMISSION = "android.permission.ACCESS_LOCAL_NETWORK"
    // Android 17's API level, the first that asks.
    private const val ASKS_FROM = 37

    fun asks() = Build.VERSION.SDK_INT >= ASKS_FROM

    fun allowed(ctx: Context) = !asks() || ctx.checkSelfPermission(PERMISSION) == PackageManager.PERMISSION_GRANTED

    fun asked(ctx: Context) = DensService.prefs(ctx).getBoolean("askedLocal", false)

    fun markAsked(ctx: Context) = DensService.prefs(ctx).edit().putBoolean("askedLocal", true).apply()
}
