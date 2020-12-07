import sys
import json
import textwrap

ordered_keys = [
    # Mode and ciphersuite parameters
    "mode", "kem_id", "kdf_id", "aead_id", "info",
    # Private key material
    "ikmE", "pkEm", "skEm",
    "ikmR", "pkRm", "skRm",
    "ikmS", "pkSm", "skSm",
    "psk", "psk_id",
    # Derived context
    "enc", "shared_secret", "key_schedule_context", "secret", "key", "base_nonce", "exporter_secret",
]

ordered_encryption_keys = [
    "plaintext", "aad", "nonce", "ciphertext",
]

encryption_count_keys = [
    0, 1, 2, 4, 10, 32, 255, 256, 257
]

def entry_kem(entry):
    return kemMap[entry["kem_id"]]

def entry_kem_value(entry):
    return entry["kem_id"]

def entry_kdf(entry):
    return kdfMap[entry["kdf_id"]]

def entry_kdf_value(entry):
    return entry["kdf_id"]

def entry_aead(entry):
    return aeadMap[entry["aead_id"]]

def entry_aead_value(entry):
    return entry["aead_id"]

def entry_mode(entry):
    return modeMap[entry["mode"]]

def entry_mode_value(entry):
    return entry["mode"]

modeBase = 0x00
modePSK = 0x01
modeAuth = 0x02
modeAuthPSK = 0x03
modeMap = {modeBase: "Base", modePSK: "PSK", modeAuth: "Auth", modeAuthPSK: "AuthPSK"}

kem_idP256 = 0x0010
kem_idP521 = 0x0012
kem_idX25519 = 0x0020
kemMap = {kem_idX25519: "DHKEM(X25519, HKDF-SHA256)", kem_idP256: "DHKEM(P-256, HKDF-SHA256)", kem_idP521: "DHKEM(P-521, HKDF-SHA512)"}

kdf_idSHA256 = 0x0001
kdf_idSHA512 = 0x0003
kdfMap = {kdf_idSHA256: "HKDF-SHA256", kdf_idSHA512: "HKDF-SHA512"}

aead_idAES128GCM = 0x0001
aead_idAES256GCM = 0x0002
aead_idChaCha20Poly1305 = 0x0003
aeadMap = {aead_idAES128GCM: "AES-128-GCM", aead_idAES256GCM: "AES-256-GCM", aead_idChaCha20Poly1305: "ChaCha20Poly1305"}

class CipherSuite(object):
    def __init__(self, kem_id, kdf_id, aead_id):
        self.kem_id = kem_id
        self.kdf_id = kdf_id
        self.aead_id = aead_id

    def __str__(self):
        return kemMap[self.kem_id] + ", " + kdfMap[self.kdf_id] + ", " + aeadMap[self.aead_id]

    def __repr__(self):
        return str(self)

    def matches_vector(self, vector):
        return self.kem_id == entry_kem_value(vector) and self.kdf_id == entry_kdf_value(vector) and self.aead_id == entry_aead_value(vector)

testSuites = [
    CipherSuite(kem_idX25519, kdf_idSHA256, aead_idAES128GCM),
    CipherSuite(kem_idX25519, kdf_idSHA256, aead_idChaCha20Poly1305),
    CipherSuite(kem_idP256, kdf_idSHA256, aead_idAES128GCM),
    CipherSuite(kem_idP256, kdf_idSHA512, aead_idAES128GCM),
    CipherSuite(kem_idP256, kdf_idSHA256, aead_idChaCha20Poly1305),
    CipherSuite(kem_idP521, kdf_idSHA512, aead_idAES256GCM),
]

def wrap_line(value):
    return textwrap.fill(value, width=72)

def format_encryption(entry, count):
    formatted = wrap_line("sequence number: %d" % count) + "\n"
    for key in ordered_encryption_keys:
        if key in entry:
            formatted = formatted + wrap_line(key + ": " + str(entry[key])) + "\n"
    return formatted

def format_encryptions(entry, mode):
    formatted = "~~~\n"
    for seq_number in encryption_count_keys:
        for i, encryption in enumerate(entry["encryptions"]):
            if i == seq_number:
                formatted = formatted + format_encryption(encryption, i)
                if i < len(entry["encryptions"]) - 1:
                    formatted = formatted + "\n"
    return formatted + "~~~"

def format_vector(entry, mode):
    formatted = "~~~\n"
    for key in ordered_keys:
        if key in entry:
            formatted = formatted + wrap_line(key + ": " + str(entry[key])) + "\n"
    return formatted + "~~~\n"

with open(sys.argv[1], "r") as fh:
    data = json.load(fh)
    for suite in testSuites:
        print("## " + str(suite))
        print("")
        for mode in [modeBase, modePSK, modeAuth, modeAuthPSK]:
            for vector in data:
                if suite.matches_vector(vector):
                    if mode == entry_mode_value(vector):
                        print("### " + modeMap[mode] + " Setup Information")
                        print(format_vector(vector, mode))
                        print("#### Encryptions")
                        print(format_encryptions(vector, mode))
                        print("")
