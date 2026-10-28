package chat.dens.spike

import android.content.Context
import android.media.AudioDeviceInfo
import android.media.AudioManager

// Audio moves a call's sound between the earpiece, the loudspeaker and a
// headset, as M12's shell would on the page's behalf, and says where it is.
object Audio {
    private fun kind(t: Int): String = when (t) {
        AudioDeviceInfo.TYPE_BUILTIN_EARPIECE -> "earpiece"
        AudioDeviceInfo.TYPE_BUILTIN_SPEAKER -> "speaker"
        AudioDeviceInfo.TYPE_BLUETOOTH_SCO -> "bluetooth"
        AudioDeviceInfo.TYPE_BLE_HEADSET -> "bluetooth LE"
        AudioDeviceInfo.TYPE_WIRED_HEADSET -> "wired headset"
        AudioDeviceInfo.TYPE_USB_HEADSET -> "USB headset"
        else -> "type $t"
    }

    fun describe(ctx: Context): String {
        val am = ctx.getSystemService(AudioManager::class.java)
        val mode = when (am.mode) {
            AudioManager.MODE_NORMAL -> "normal"
            AudioManager.MODE_IN_COMMUNICATION -> "in communication"
            AudioManager.MODE_IN_CALL -> "in call"
            else -> "mode ${am.mode}"
        }
        val now = am.communicationDevice?.let { kind(it.type) } ?: "none"
        val all = am.availableCommunicationDevices.joinToString { kind(it.type) }
        return "audio mode $mode, communication device $now, available: $all"
    }

    fun route(ctx: Context, want: String): String {
        val am = ctx.getSystemService(AudioManager::class.java)
        if (want == "clear") {
            am.clearCommunicationDevice()
            return "cleared; " + describe(ctx)
        }
        val dev = am.availableCommunicationDevices.firstOrNull { kind(it.type).startsWith(want) }
            ?: return "no $want; " + describe(ctx)
        val ok = am.setCommunicationDevice(dev)
        return "set $want: $ok; " + describe(ctx)
    }
}
