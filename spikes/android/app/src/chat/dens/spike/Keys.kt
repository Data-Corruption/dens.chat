package chat.dens.spike

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import java.io.File
import java.security.KeyStore
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.GCMParameterSpec

// Keys wraps a 32-byte key, as Dens's data key would be, with an AES key
// the Keystore holds, in StrongBox where the phone has one, and unwraps it
// for the probe, which gets it on its standard input.
object Keys {
    private const val ALIAS = "dens-spike"

    private fun file(ctx: Context) = File(Spike.dir(ctx), "datakey.wrapped")

    private fun keystore(): KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    private fun generate(strongBox: Boolean): SecretKey {
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .setIsStrongBoxBacked(strongBox)
                .build()
        )
        return gen.generateKey()
    }

    // ensure makes the Keystore key and the wrapped key once, and says
    // where the key lives and what each step took.
    fun ensure(ctx: Context): String {
        val ks = keystore()
        val notes = StringBuilder()
        var key = ks.getKey(ALIAS, null) as SecretKey?
        if (key == null || !file(ctx).exists()) {
            ks.deleteEntry(ALIAS)
            val hasStrongBox = ctx.packageManager.hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE)
            val t0 = System.nanoTime()
            key = try {
                generate(hasStrongBox)
            } catch (e: StrongBoxUnavailableException) {
                notes.append("StrongBox refused: $e\n")
                generate(false)
            }
            notes.append("made the key in ${(System.nanoTime() - t0) / 1_000_000} ms (StrongBox feature: $hasStrongBox)\n")
            val plain = ByteArray(32).also { SecureRandom().nextBytes(it) }
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            val t1 = System.nanoTime()
            cipher.init(Cipher.ENCRYPT_MODE, key)
            val sealed = cipher.doFinal(plain)
            notes.append("wrapped 32 bytes in ${(System.nanoTime() - t1) / 1_000_000} ms\n")
            file(ctx).writeBytes(byteArrayOf(cipher.iv.size.toByte()) + cipher.iv + sealed)
            plain.fill(0)
        }
        val info = SecretKeyFactory.getInstance(key!!.algorithm, "AndroidKeyStore").getKeySpec(key, KeyInfo::class.java) as KeyInfo
        val level = when (info.securityLevel) {
            KeyProperties.SECURITY_LEVEL_STRONGBOX -> "StrongBox"
            KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> "TEE"
            KeyProperties.SECURITY_LEVEL_SOFTWARE -> "software"
            else -> "level ${info.securityLevel}"
        }
        val unlocked = if (Build.VERSION.SDK_INT >= 37) info.isUnlockedDeviceRequired.toString() else "?"
        notes.append("Keystore key: $level, user auth ${info.isUserAuthenticationRequired}, unlocked device required $unlocked")
        return notes.toString()
    }

    // unwrap returns the 32 bytes and what unwrapping took; the caller
    // zeroes them.
    fun unwrap(ctx: Context): Pair<ByteArray, Long> {
        val t0 = System.nanoTime()
        val key = keystore().getKey(ALIAS, null) as SecretKey
        val all = file(ctx).readBytes()
        val ivLen = all[0].toInt()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, all, 1, ivLen))
        val plain = cipher.doFinal(all, 1 + ivLen, all.size - 1 - ivLen)
        return Pair(plain, (System.nanoTime() - t0) / 1_000_000)
    }

    // prefix is the hash prefix the probe prints for the same bytes.
    fun prefix(b: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(b).take(4).joinToString("") { "%02x".format(it) }
}
