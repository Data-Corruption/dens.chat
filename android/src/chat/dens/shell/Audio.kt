package chat.dens.shell

import android.content.Context
import android.media.AudioDeviceInfo
import android.media.AudioManager

// Audio is where a call's sound plays (M12): Android's communication
// device, which the page's call follows, since the page plays it through an
// audio context made once the microphone is open, which Android plays as a
// call's. A call starts on a headset where one is connected and on the
// loudspeaker otherwise, as the WebView's Chromium starts it too; the
// call's notification offers the others.
object Audio {
    // Route is a place a call's sound may play, by the device types that
    // are it, in the order the notification offers them; a headset comes
    // and goes.
    enum class Route(val label: String, val where: String, val headset: Boolean, val types: Set<Int>) {
        LOUDSPEAKER("Loudspeaker", "on the loudspeaker", false, setOf(AudioDeviceInfo.TYPE_BUILTIN_SPEAKER)),
        EARPIECE("Earpiece", "on the earpiece", false, setOf(AudioDeviceInfo.TYPE_BUILTIN_EARPIECE)),
        BLUETOOTH("Bluetooth", "on Bluetooth", true, setOf(
            AudioDeviceInfo.TYPE_BLUETOOTH_SCO, AudioDeviceInfo.TYPE_BLE_HEADSET, AudioDeviceInfo.TYPE_BLE_SPEAKER, AudioDeviceInfo.TYPE_HEARING_AID,
        )),
        HEADSET("Headset", "on the headset", true, setOf(AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_USB_HEADSET)),
    }

    // A call takes a wired headset first, then a Bluetooth one, then the
    // loudspeaker, as Chromium does; the earpiece only when asked for.
    private val byItself = listOf(Route.HEADSET, Route.BLUETOOTH, Route.LOUDSPEAKER)

    private fun manager(ctx: Context) = ctx.getSystemService(AudioManager::class.java)

    fun routeOf(d: AudioDeviceInfo?): Route? = d?.let { dev -> Route.entries.firstOrNull { dev.type in it.types } }

    // available is where a call's sound may play now.
    fun available(ctx: Context): Set<Route> = manager(ctx).availableCommunicationDevices.mapNotNull { routeOf(it) }.toSet()

    // current is where a call's sound plays now, as Android says.
    fun current(ctx: Context): Route? = routeOf(manager(ctx).communicationDevice)

    // choose is where a call's sound goes by itself.
    fun choose(ctx: Context): Route? {
        val here = available(ctx)
        return byItself.firstOrNull { it in here }
    }

    // set has a call's sound play on route, and reports whether Android
    // took it. A Bluetooth headset takes a moment more to come up.
    fun set(ctx: Context, route: Route): Boolean {
        val am = manager(ctx)
        val device = am.availableCommunicationDevices.firstOrNull { it.type in route.types } ?: return false
        return am.setCommunicationDevice(device)
    }

    fun clear(ctx: Context) = manager(ctx).clearCommunicationDevice()
}
