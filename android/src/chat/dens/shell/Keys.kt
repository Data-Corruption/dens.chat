package chat.dens.shell

import android.content.Context
import android.content.pm.PackageManager
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import android.system.Os
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.security.KeyStore
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.GCMParameterSpec

// Keys keeps Dens's data key wrapped with an AES key the Keystore holds, in
// StrongBox where the phone has one, whose material never leaves it (M12).
// The shell makes the data key as the instance is made, and unwraps it
// each time it starts the service, which reads it on its standard input.
object Keys {
    private const val ALIAS = "dens-data-key"
    private const val KEY_SIZE = 32
    // The wrapped key's file holds this format's byte, the IV's length and
    // the IV, then the sealed key and its tag, sealed with the format and
    // what the key is for as associated data.
    private const val FORMAT: Byte = 1
    private val AD = "dens data key".toByteArray()

    // Lost is a data key the shell can't give the service: making another
    // would leave the data with nothing to open them.
    class Lost(message: String) : Exception(message)

    // dataKey returns the data key, making it first for a new instance.
    // The caller clears it.
    fun dataKey(ctx: Context): ByteArray {
        val file = Dens.wrappedKey(ctx)
        val keystore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        if (!file.exists()) {
            if (Dens.state(ctx).exists()) throw Lost("Dens's data is on this phone, but the key that opens it is gone.")
            return make(ctx, keystore, file)
        }
        val key = keystore.getKey(ALIAS, null) as SecretKey?
            ?: throw Lost("The key that opens Dens's data is gone from this phone's keystore.")
        val all = file.readBytes()
        if (all.size < 2 || all[0] != FORMAT || all.size < 2 + all[1]) throw Lost("Dens's wrapped key isn't one this version reads.")
        val ivSize = all[1].toInt()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, all, 2, ivSize))
        cipher.updateAAD(AD)
        val plain = try {
            cipher.doFinal(all, 2 + ivSize, all.size - 2 - ivSize)
        } catch (e: Exception) {
            throw Lost("The phone's keystore didn't open Dens's key: ${e.javaClass.simpleName}")
        }
        if (plain.size != KEY_SIZE) {
            plain.fill(0)
            throw Lost("Dens's wrapped key isn't one this version reads.")
        }
        return plain
    }

    private fun make(ctx: Context, keystore: KeyStore, file: File): ByteArray {
        // A Keystore key left by an instance before has nothing to open now.
        keystore.deleteEntry(ALIAS)
        val strongBox = ctx.packageManager.hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE)
        val key = try {
            generate(strongBox)
        } catch (e: StrongBoxUnavailableException) {
            generate(false)
        }
        val plain = ByteArray(KEY_SIZE).also { SecureRandom().nextBytes(it) }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key)
        cipher.updateAAD(AD)
        val sealed = cipher.doFinal(plain)
        val iv = cipher.iv
        val temp = File(Dens.controlDir(ctx), ".datakey.keystore.tmp")
        temp.delete()
        FileOutputStream(temp).use { out ->
            out.write(byteArrayOf(FORMAT, iv.size.toByte()) + iv + sealed)
            out.fd.sync()
        }
        Os.chmod(temp.path, "600".toInt(8))
        if (!temp.renameTo(file)) {
            plain.fill(0)
            throw IOException("couldn't keep the wrapped key")
        }
        Dens.log(ctx, "keys: made the data key, wrapped by a key in ${where(key)}")
        return plain
    }

    // where says what holds a Keystore key.
    private fun where(key: SecretKey): String {
        val info = SecretKeyFactory.getInstance(key.algorithm, "AndroidKeyStore").getKeySpec(key, KeyInfo::class.java) as KeyInfo
        return when (info.securityLevel) {
            KeyProperties.SECURITY_LEVEL_STRONGBOX -> "StrongBox"
            KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> "the TEE"
            KeyProperties.SECURITY_LEVEL_SOFTWARE -> "software"
            else -> "security level ${info.securityLevel}"
        }
    }

    // generate makes the AES key in the Keystore: one no other app may use,
    // that opens with the phone locked, as after a restart in a pocket.
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
}
