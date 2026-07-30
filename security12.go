package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	securityEnvelopeV1 = "security-v1"

	signingAlgorithmEd25519      = "ed25519"
	encryptionAlgorithmX25519GCM = "x25519-aes256gcm-v1"
)

const (
	securityCodeKeyNotFound                    = "key_not_found"
	securityCodeInvalidKeyIdentifier           = "invalid_key_identifier"
	securityCodeInvalidKeyFormat               = "invalid_key_format"
	securityCodeInvalidKeyPurpose              = "invalid_key_purpose"
	securityCodeDuplicateKeyIdentifier         = "duplicate_key_identifier"
	securityCodePrivateKeyUnavailable          = "private_key_unavailable"
	securityCodePrivateKeyLocked               = "private_key_locked"
	securityCodePrivateKeyPermissionUnsafe     = "private_key_permission_unsafe"
	securityCodeKeyImportFailed                = "key_import_failed"
	securityCodeKeyExportFailed                = "key_export_failed"
	securityCodeKeyGenerationFailed            = "key_generation_failed"
	securityCodeKeyRemovalFailed               = "key_removal_failed"
	securityCodeInvalidTrustState              = "invalid_trust_state"
	securityCodeTrustRecordNotFound            = "trust_record_not_found"
	securityCodeSigningFailed                  = "signing_failed"
	securityCodeSignatureInvalid               = "signature_invalid"
	securityCodeSignatureMissing               = "signature_missing"
	securityCodeSignatureUntrusted             = "signature_untrusted"
	securityCodeSignerRevoked                  = "signer_revoked"
	securityCodeRequiredSignerMissing          = "required_signer_missing"
	securityCodeUnsupportedSignatureAlgorithm  = "unsupported_signature_algorithm"
	securityCodeEncryptionFailed               = "encryption_failed"
	securityCodeDecryptionFailed               = "decryption_failed"
	securityCodeRecipientKeyNotFound           = "recipient_key_not_found"
	securityCodeRecipientPrivateKeyUnavailable = "recipient_private_key_unavailable"
	securityCodeUnsupportedEncryptionAlgorithm = "unsupported_encryption_algorithm"
	securityCodeInvalidSecurityEnvelope        = "invalid_security_envelope"
	securityCodeUnsupportedSecurityEnvelope    = "unsupported_security_envelope"
	securityCodeEncryptedBundleNeedsDecrypt    = "encrypted_bundle_requires_decryption"
	securityCodeBundlePolicyFailed             = "bundle_security_policy_failed"
	securityCodeBundleReencryptFailed          = "bundle_reencryption_failed"
)

const (
	keyPurposeSigning    = "signing"
	keyPurposeEncryption = "encryption"
)

const (
	trustStateTrusted   = "trusted"
	trustStateUntrusted = "untrusted"
	trustStateRevoked   = "revoked"
	trustStateUnknown   = "unknown"
)

type securityError struct {
	Code    string
	Message string
}

func (err securityError) Error() string {
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("code=%s %s", err.Code, err.Message)
}

type keyPublicRecord struct {
	Version     string `json:"version"`
	Purpose     string `json:"purpose"`
	Identifier  string `json:"id"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	CreatedAt   string `json:"created_at"`
	Source      string `json:"source,omitempty"`
}

type keyPrivateRecord struct {
	Version       string `json:"version"`
	Purpose       string `json:"purpose"`
	Identifier    string `json:"id"`
	Algorithm     string `json:"algorithm"`
	Fingerprint   string `json:"fingerprint"`
	EncryptedBlob string `json:"encrypted_blob"`
	KDF           string `json:"kdf"`
	KDFSalt       string `json:"kdf_salt"`
	KDFN          int    `json:"kdf_n"`
	KDFR          int    `json:"kdf_r"`
	KDFP          int    `json:"kdf_p"`
	Nonce         string `json:"nonce"`
	PassphraseRef string `json:"passphrase_ref,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type keyIndexEntry struct {
	Identifier   string `json:"id"`
	Purpose      string `json:"purpose"`
	Algorithm    string `json:"algorithm"`
	Fingerprint  string `json:"fingerprint"`
	HasPublic    bool   `json:"has_public"`
	HasPrivate   bool   `json:"has_private"`
	TrustState   string `json:"trust_state"`
	Source       string `json:"source"`
	PublicPath   string `json:"public_path,omitempty"`
	PrivatePath  string `json:"private_path,omitempty"`
	InvalidFiles []string
}

type trustRecord struct {
	State      string   `json:"state"`
	Supersedes []string `json:"supersedes,omitempty"`
	UpdatedAt  string   `json:"updated_at"`
}

type trustFile struct {
	Version string                 `json:"version"`
	Records map[string]trustRecord `json:"records"`
}

type bundleSecurityEnvelope struct {
	Version    string                   `json:"version"`
	Encrypted  bool                     `json:"encrypted,omitempty"`
	Encryption *bundleEncryptionDetails `json:"encryption,omitempty"`
	Signatures []bundleSignature        `json:"signatures,omitempty"`
}

type bundleEncryptionDetails struct {
	Algorithm      string            `json:"algorithm"`
	PayloadNonce   string            `json:"payload_nonce"`
	PayloadDigest  string            `json:"payload_digest"`
	Recipients     []bundleRecipient `json:"recipients"`
	Authenticated  map[string]string `json:"authenticated_metadata,omitempty"`
	SecurityFormat string            `json:"security_format"`
}

type bundleRecipient struct {
	KeyID           string `json:"key_id,omitempty"`
	Fingerprint     string `json:"fingerprint"`
	EphemeralKey    string `json:"ephemeral_public_key"`
	WrappedKey      string `json:"wrapped_key"`
	WrappedKeyNonce string `json:"wrapped_key_nonce"`
}

type bundleSignature struct {
	Algorithm         string `json:"algorithm"`
	SignerKeyID       string `json:"signer_key_id"`
	SignerFingerprint string `json:"signer_fingerprint"`
	CreatedAt         string `json:"created_at"`
	SecurityVersion   string `json:"security_version"`
	CoveredDigest     string `json:"covered_digest"`
	Signature         string `json:"signature"`
}

type encryptedBundlePayload struct {
	Manifest  bundleManifest                 `json:"manifest"`
	Resources []encryptedBundleResourceEntry `json:"resources"`
}

type encryptedBundleResourceEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type keyUnlockOptions struct {
	PassphraseLiteral string
	PassphraseRef     string
}

type signatureVerificationResult struct {
	SignerKeyID       string `json:"signer_key_id"`
	SignerFingerprint string `json:"signer_fingerprint"`
	Algorithm         string `json:"algorithm"`
	Timestamp         string `json:"timestamp"`
	CoveredDigest     string `json:"covered_digest"`
	Valid             bool   `json:"valid"`
	TrustState        string `json:"trust_state"`
	Code              string `json:"code,omitempty"`
	Message           string `json:"message,omitempty"`
}

type bundleSecurityVerification struct {
	Signed                bool                          `json:"signed"`
	Encrypted             bool                          `json:"encrypted"`
	EnvelopeVersion       string                        `json:"envelope_version,omitempty"`
	Signatures            []signatureVerificationResult `json:"signatures"`
	HasTrustedValid       bool                          `json:"has_trusted_valid_signature"`
	HasAnyValid           bool                          `json:"has_any_valid_signature"`
	RecipientFingerprints []string                      `json:"recipient_fingerprints,omitempty"`
}

type importTrustPolicy struct {
	Mode            string
	RequiredSigners []string
}

func keyRegistryRoot(paths Paths) string {
	return filepath.Join(paths.ConfigHome, "keys")
}

func keyPurposeDir(paths Paths, purpose string) (string, error) {
	switch purpose {
	case keyPurposeSigning, keyPurposeEncryption:
		return filepath.Join(keyRegistryRoot(paths), purpose), nil
	default:
		return "", securityError{Code: securityCodeInvalidKeyPurpose, Message: fmt.Sprintf("invalid key purpose %q", purpose)}
	}
}

func keyPublicPath(paths Paths, purpose string, keyID string) (string, error) {
	base, err := keyPurposeDir(paths, purpose)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "public", keyID+".json"), nil
}

func keyPrivatePath(paths Paths, purpose string, keyID string) (string, error) {
	base, err := keyPurposeDir(paths, purpose)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "private", keyID+".json"), nil
}

func keyIdentifierValid(keyID string) bool {
	if strings.TrimSpace(keyID) == "" {
		return false
	}
	pattern := regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	return pattern.MatchString(keyID)
}

func validateKeyIdentifier(keyID string) error {
	if !keyIdentifierValid(keyID) {
		return securityError{Code: securityCodeInvalidKeyIdentifier, Message: "key identifier must match ^[A-Za-z0-9._:-]+$"}
	}
	return nil
}

func createParentDir(path string, mode os.FileMode) error {
	return os.MkdirAll(filepath.Dir(path), mode)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode, overwrite bool) error {
	if !overwrite && fileExists(path) {
		return securityError{Code: securityCodeDuplicateKeyIdentifier, Message: fmt.Sprintf("file already exists: %s", path)}
	}
	if err := createParentDir(path, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".aidev-write-*.tmp")
	if err != nil {
		return err
	}
	tmp := temp.Name()
	defer func() {
		_ = os.Remove(tmp)
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func fingerprintForPublicKey(algorithm string, publicKey []byte) string {
	canonical := algorithm + ":" + base64.StdEncoding.EncodeToString(publicKey)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func encodeJSON(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}

func resolvePassphrase(paths Paths, options keyUnlockOptions) (string, error) {
	if options.PassphraseLiteral != "" {
		return options.PassphraseLiteral, nil
	}
	if options.PassphraseRef == "" {
		return "", securityError{Code: securityCodePrivateKeyLocked, Message: "private key is protected and no passphrase was provided"}
	}
	reference, err := parseSecretReference(options.PassphraseRef)
	if err != nil {
		return "", securityError{Code: securityCodePrivateKeyLocked, Message: "invalid passphrase secret reference"}
	}
	definitions := map[string]SecretCommandDefinition{}
	if info, infoErr := resolveProjectInfo(paths); infoErr == nil {
		if resolved, _, resolveErr := resolveConfiguration(paths, info); resolveErr == nil {
			definitions = loadSecretCommandDefinitions(resolved)
		}
	}
	resolver := newProjectSecretResolver(paths, definitions)
	value, err := resolver.Resolve(context.Background(), reference)
	if err != nil {
		return "", securityError{Code: securityCodePrivateKeyLocked, Message: "cannot unlock private key with provided secret reference"}
	}
	if strings.TrimSpace(value) == "" {
		return "", securityError{Code: securityCodePrivateKeyLocked, Message: "empty private-key passphrase"}
	}
	return value, nil
}

func derivePrivateKeyCipher(passphrase string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(passphrase), salt, 32768, 8, 1, 32)
}

func encryptPrivateKeyMaterial(privateBytes []byte, options keyUnlockOptions, paths Paths) (keyPrivateRecord, error) {
	passphrase, err := resolvePassphrase(paths, options)
	if err != nil {
		return keyPrivateRecord{}, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return keyPrivateRecord{}, securityError{Code: securityCodeKeyGenerationFailed, Message: "generate key salt failed"}
	}
	key, err := derivePrivateKeyCipher(passphrase, salt)
	if err != nil {
		return keyPrivateRecord{}, securityError{Code: securityCodeKeyGenerationFailed, Message: "derive key encryption material failed"}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return keyPrivateRecord{}, securityError{Code: securityCodeKeyGenerationFailed, Message: "create key encryption cipher failed"}
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return keyPrivateRecord{}, securityError{Code: securityCodeKeyGenerationFailed, Message: "create key encryption mode failed"}
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return keyPrivateRecord{}, securityError{Code: securityCodeKeyGenerationFailed, Message: "generate key nonce failed"}
	}
	ciphertext := aead.Seal(nil, nonce, privateBytes, []byte("ai-dev-private-key"))
	return keyPrivateRecord{
		Version:       securityEnvelopeV1,
		EncryptedBlob: base64.StdEncoding.EncodeToString(ciphertext),
		KDF:           "scrypt",
		KDFSalt:       base64.StdEncoding.EncodeToString(salt),
		KDFN:          32768,
		KDFR:          8,
		KDFP:          1,
		Nonce:         base64.StdEncoding.EncodeToString(nonce),
		PassphraseRef: options.PassphraseRef,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func decryptPrivateKeyMaterial(paths Paths, record keyPrivateRecord, options keyUnlockOptions) ([]byte, error) {
	if record.Version != securityEnvelopeV1 {
		return nil, securityError{Code: securityCodeUnsupportedSecurityEnvelope, Message: fmt.Sprintf("unsupported private-key envelope version %q", record.Version)}
	}
	if record.KDF != "scrypt" {
		return nil, securityError{Code: securityCodeInvalidKeyFormat, Message: "unsupported private-key KDF"}
	}
	if options.PassphraseRef == "" && record.PassphraseRef != "" {
		options.PassphraseRef = record.PassphraseRef
	}
	passphrase, err := resolvePassphrase(paths, options)
	if err != nil {
		return nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(record.KDFSalt)
	if err != nil {
		return nil, securityError{Code: securityCodeInvalidKeyFormat, Message: "invalid private-key KDF salt"}
	}
	nonce, err := base64.StdEncoding.DecodeString(record.Nonce)
	if err != nil {
		return nil, securityError{Code: securityCodeInvalidKeyFormat, Message: "invalid private-key nonce"}
	}
	ciphertext, err := base64.StdEncoding.DecodeString(record.EncryptedBlob)
	if err != nil {
		return nil, securityError{Code: securityCodeInvalidKeyFormat, Message: "invalid private-key ciphertext"}
	}
	key, err := derivePrivateKeyCipher(passphrase, salt)
	if err != nil {
		return nil, securityError{Code: securityCodePrivateKeyLocked, Message: "unable to derive key decryption material"}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, securityError{Code: securityCodePrivateKeyLocked, Message: "unable to initialize key decryption cipher"}
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, securityError{Code: securityCodePrivateKeyLocked, Message: "unable to initialize key decryption mode"}
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte("ai-dev-private-key"))
	if err != nil {
		return nil, securityError{Code: securityCodePrivateKeyLocked, Message: "private key cannot be unlocked"}
	}
	return plaintext, nil
}

func loadPublicKeyRecord(paths Paths, purpose string, keyID string) (keyPublicRecord, error) {
	path, err := keyPublicPath(paths, purpose, keyID)
	if err != nil {
		return keyPublicRecord{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return keyPublicRecord{}, securityError{Code: securityCodeKeyNotFound, Message: fmt.Sprintf("key %q not found", keyID)}
		}
		return keyPublicRecord{}, securityError{Code: securityCodeInvalidKeyFormat, Message: fmt.Sprintf("read key %q failed", keyID)}
	}
	record := keyPublicRecord{}
	if err := json.Unmarshal(content, &record); err != nil {
		return keyPublicRecord{}, securityError{Code: securityCodeInvalidKeyFormat, Message: fmt.Sprintf("public key %q is invalid JSON", keyID)}
	}
	return record, nil
}

func loadPrivateKeyRecord(paths Paths, purpose string, keyID string) (keyPrivateRecord, error) {
	path, err := keyPrivatePath(paths, purpose, keyID)
	if err != nil {
		return keyPrivateRecord{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return keyPrivateRecord{}, securityError{Code: securityCodePrivateKeyUnavailable, Message: fmt.Sprintf("private key for %q is unavailable", keyID)}
		}
		return keyPrivateRecord{}, securityError{Code: securityCodeInvalidKeyFormat, Message: fmt.Sprintf("read private key %q failed", keyID)}
	}
	record := keyPrivateRecord{}
	if err := json.Unmarshal(content, &record); err != nil {
		return keyPrivateRecord{}, securityError{Code: securityCodeInvalidKeyFormat, Message: fmt.Sprintf("private key %q is invalid JSON", keyID)}
	}
	return record, nil
}

func listAllKeyEntries(paths Paths) ([]keyIndexEntry, error) {
	entries := map[string]*keyIndexEntry{}
	purposes := []string{keyPurposeSigning, keyPurposeEncryption}
	for _, purpose := range purposes {
		for _, visibility := range []string{"public", "private"} {
			directory := filepath.Join(keyRegistryRoot(paths), purpose, visibility)
			dirEntries, err := os.ReadDir(directory)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			for _, dirEntry := range dirEntries {
				if dirEntry.IsDir() || !strings.HasSuffix(dirEntry.Name(), ".json") {
					continue
				}
				keyID := strings.TrimSuffix(dirEntry.Name(), ".json")
				entry, ok := entries[purpose+"\x00"+keyID]
				if !ok {
					entry = &keyIndexEntry{Identifier: keyID, Purpose: purpose, TrustState: trustStateUnknown, Source: "local"}
					entries[purpose+"\x00"+keyID] = entry
				}
				if visibility == "public" {
					entry.HasPublic = true
					path, _ := keyPublicPath(paths, purpose, keyID)
					entry.PublicPath = path
					record, err := loadPublicKeyRecord(paths, purpose, keyID)
					if err == nil {
						entry.Fingerprint = record.Fingerprint
						entry.Algorithm = record.Algorithm
					}
				} else {
					entry.HasPrivate = true
					path, _ := keyPrivatePath(paths, purpose, keyID)
					entry.PrivatePath = path
				}
			}
		}
	}
	result := make([]keyIndexEntry, 0, len(entries))
	for _, entry := range entries {
		state, _, _ := effectiveTrustState(paths, entry.Identifier)
		entry.TrustState = state
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Purpose != result[j].Purpose {
			return result[i].Purpose < result[j].Purpose
		}
		return result[i].Identifier < result[j].Identifier
	})
	return result, nil
}

func trustFilePath(paths Paths, scope string) (string, error) {
	switch scope {
	case "global":
		return filepath.Join(paths.ConfigHome, "trust", "global.json"), nil
	case "project":
		info, err := resolveProjectInfo(paths)
		if err != nil {
			return "", err
		}
		return filepath.Join(paths.ConfigHome, "trust", "projects", safeProjectFilename(info.ProjectID)+".json"), nil
	default:
		return "", UsageError{Message: "trust scope must be global or project"}
	}
}

func loadTrustFile(paths Paths, scope string) (trustFile, error) {
	path, err := trustFilePath(paths, scope)
	if err != nil {
		return trustFile{}, err
	}
	if !fileExists(path) {
		return trustFile{Version: securityEnvelopeV1, Records: map[string]trustRecord{}}, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return trustFile{}, err
	}
	file := trustFile{}
	if err := json.Unmarshal(content, &file); err != nil {
		return trustFile{}, securityError{Code: securityCodeInvalidTrustState, Message: fmt.Sprintf("invalid trust file: %s", path)}
	}
	if file.Records == nil {
		file.Records = map[string]trustRecord{}
	}
	if file.Version == "" {
		file.Version = securityEnvelopeV1
	}
	return file, nil
}

func saveTrustFile(paths Paths, scope string, file trustFile) error {
	path, err := trustFilePath(paths, scope)
	if err != nil {
		return err
	}
	if file.Version == "" {
		file.Version = securityEnvelopeV1
	}
	if file.Records == nil {
		file.Records = map[string]trustRecord{}
	}
	content, err := encodeJSON(file)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, content, 0o600, true)
}

func validTrustState(state string) bool {
	switch state {
	case trustStateTrusted, trustStateUntrusted, trustStateRevoked, trustStateUnknown:
		return true
	default:
		return false
	}
}

func effectiveTrustState(paths Paths, keyID string) (string, string, error) {
	project, _ := loadTrustFile(paths, "project")
	if record, ok := project.Records[keyID]; ok {
		if validTrustState(record.State) {
			return record.State, "project", nil
		}
	}
	global, _ := loadTrustFile(paths, "global")
	if record, ok := global.Records[keyID]; ok {
		if validTrustState(record.State) {
			return record.State, "global", nil
		}
	}
	return trustStateUnknown, "default", nil
}

func keyCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "key requires a subcommand"}
	}
	switch arguments[0] {
	case "generate":
		return keyGenerateCommand(paths, arguments[1:])
	case "import":
		return keyImportCommand(paths, arguments[1:])
	case "export":
		return keyExportCommand(paths, arguments[1:])
	case "list":
		return keyListCommand(paths, arguments[1:])
	case "show":
		return keyShowCommand(paths, arguments[1:])
	case "remove":
		return keyRemoveCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown key subcommand: %s", arguments[0])}
	}
}

func keyGenerateCommand(paths Paths, arguments []string) error {
	purpose := ""
	keyID := ""
	unlock := keyUnlockOptions{}
	for i := 0; i < len(arguments); i++ {
		switch arguments[i] {
		case "--purpose":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--purpose requires a value"}
			}
			i++
			purpose = arguments[i]
		case "--id":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--id requires a value"}
			}
			i++
			keyID = arguments[i]
		case "--passphrase":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase requires a value"}
			}
			i++
			unlock.PassphraseLiteral = arguments[i]
		case "--passphrase-ref":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase-ref requires a value"}
			}
			i++
			unlock.PassphraseRef = arguments[i]
		default:
			return UsageError{Message: fmt.Sprintf("unknown key generate option: %s", arguments[i])}
		}
	}
	if purpose == "" {
		return UsageError{Message: "--purpose is required"}
	}
	if err := validateKeyIdentifier(keyID); err != nil {
		return err
	}
	if unlock.PassphraseLiteral == "" && unlock.PassphraseRef == "" {
		return securityError{Code: securityCodePrivateKeyLocked, Message: "private-key protection is required; specify --passphrase or --passphrase-ref"}
	}
	switch purpose {
	case keyPurposeSigning:
		return generateSigningKey(paths, keyID, unlock)
	case keyPurposeEncryption:
		return generateEncryptionKey(paths, keyID, unlock)
	default:
		return securityError{Code: securityCodeInvalidKeyPurpose, Message: fmt.Sprintf("invalid purpose %q", purpose)}
	}
}

func generateSigningKey(paths Paths, keyID string, unlock keyUnlockOptions) error {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return securityError{Code: securityCodeKeyGenerationFailed, Message: "generate Ed25519 keypair failed"}
	}
	fingerprint := fingerprintForPublicKey(signingAlgorithmEd25519, publicKey)
	publicRecord := keyPublicRecord{
		Version:     securityEnvelopeV1,
		Purpose:     keyPurposeSigning,
		Identifier:  keyID,
		Algorithm:   signingAlgorithmEd25519,
		Fingerprint: fingerprint,
		PublicKey:   base64.StdEncoding.EncodeToString(publicKey),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Source:      "generated",
	}
	privateEnvelope, err := encryptPrivateKeyMaterial(privateKey, unlock, paths)
	if err != nil {
		return err
	}
	privateEnvelope.Purpose = keyPurposeSigning
	privateEnvelope.Identifier = keyID
	privateEnvelope.Algorithm = signingAlgorithmEd25519
	privateEnvelope.Fingerprint = fingerprint

	publicPath, _ := keyPublicPath(paths, keyPurposeSigning, keyID)
	privatePath, _ := keyPrivatePath(paths, keyPurposeSigning, keyID)
	if fileExists(publicPath) || fileExists(privatePath) {
		return securityError{Code: securityCodeDuplicateKeyIdentifier, Message: fmt.Sprintf("key %q already exists", keyID)}
	}
	publicJSON, _ := encodeJSON(publicRecord)
	privateJSON, _ := encodeJSON(privateEnvelope)
	if err := writeFileAtomic(publicPath, publicJSON, 0o644, false); err != nil {
		return securityError{Code: securityCodeKeyGenerationFailed, Message: err.Error()}
	}
	if err := writeFileAtomic(privatePath, privateJSON, 0o600, false); err != nil {
		_ = os.Remove(publicPath)
		return securityError{Code: securityCodeKeyGenerationFailed, Message: err.Error()}
	}
	fmt.Printf("generated key id=%s purpose=%s fingerprint=%s\n", keyID, keyPurposeSigning, fingerprint)
	return nil
}

func generateEncryptionKey(paths Paths, keyID string, unlock keyUnlockOptions) error {
	curve := ecdh.X25519()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return securityError{Code: securityCodeKeyGenerationFailed, Message: "generate encryption keypair failed"}
	}
	publicKey := privateKey.PublicKey().Bytes()
	fingerprint := fingerprintForPublicKey(encryptionAlgorithmX25519GCM, publicKey)
	publicRecord := keyPublicRecord{
		Version:     securityEnvelopeV1,
		Purpose:     keyPurposeEncryption,
		Identifier:  keyID,
		Algorithm:   encryptionAlgorithmX25519GCM,
		Fingerprint: fingerprint,
		PublicKey:   base64.StdEncoding.EncodeToString(publicKey),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Source:      "generated",
	}
	privateEnvelope, err := encryptPrivateKeyMaterial(privateKey.Bytes(), unlock, paths)
	if err != nil {
		return err
	}
	privateEnvelope.Purpose = keyPurposeEncryption
	privateEnvelope.Identifier = keyID
	privateEnvelope.Algorithm = encryptionAlgorithmX25519GCM
	privateEnvelope.Fingerprint = fingerprint

	publicPath, _ := keyPublicPath(paths, keyPurposeEncryption, keyID)
	privatePath, _ := keyPrivatePath(paths, keyPurposeEncryption, keyID)
	if fileExists(publicPath) || fileExists(privatePath) {
		return securityError{Code: securityCodeDuplicateKeyIdentifier, Message: fmt.Sprintf("key %q already exists", keyID)}
	}
	publicJSON, _ := encodeJSON(publicRecord)
	privateJSON, _ := encodeJSON(privateEnvelope)
	if err := writeFileAtomic(publicPath, publicJSON, 0o644, false); err != nil {
		return securityError{Code: securityCodeKeyGenerationFailed, Message: err.Error()}
	}
	if err := writeFileAtomic(privatePath, privateJSON, 0o600, false); err != nil {
		_ = os.Remove(publicPath)
		return securityError{Code: securityCodeKeyGenerationFailed, Message: err.Error()}
	}
	fmt.Printf("generated key id=%s purpose=%s fingerprint=%s\n", keyID, keyPurposeEncryption, fingerprint)
	return nil
}

func keyListCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown key list option: %s", argument)}
		}
	}
	entries, err := listAllKeyEntries(paths)
	if err != nil {
		return err
	}
	if jsonOutput {
		content, _ := encodeJSON(map[string]any{"keys": entries})
		fmt.Println(string(content))
		return nil
	}
	for _, entry := range entries {
		fmt.Printf("id=%s purpose=%s public=%t private=%t fingerprint=%s trust=%s source=%s\n", entry.Identifier, entry.Purpose, entry.HasPublic, entry.HasPrivate, entry.Fingerprint, entry.TrustState, entry.Source)
	}
	return nil
}

func resolveKeyPurpose(paths Paths, keyID string, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, purpose := range []string{keyPurposeSigning, keyPurposeEncryption} {
		publicPath, _ := keyPublicPath(paths, purpose, keyID)
		privatePath, _ := keyPrivatePath(paths, purpose, keyID)
		if fileExists(publicPath) || fileExists(privatePath) {
			return purpose, nil
		}
	}
	return "", securityError{Code: securityCodeKeyNotFound, Message: fmt.Sprintf("key %q not found", keyID)}
}

func keyShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "key show requires a key identifier"}
	}
	keyID := arguments[0]
	jsonOutput := false
	purpose := ""
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(argument, "--purpose=") {
				purpose = strings.TrimPrefix(argument, "--purpose=")
			} else {
				return UsageError{Message: fmt.Sprintf("unknown key show option: %s", argument)}
			}
		}
	}
	resolvedPurpose, err := resolveKeyPurpose(paths, keyID, purpose)
	if err != nil {
		return err
	}
	publicRecord, err := loadPublicKeyRecord(paths, resolvedPurpose, keyID)
	if err != nil {
		return err
	}
	privatePath, _ := keyPrivatePath(paths, resolvedPurpose, keyID)
	_, privateErr := os.Stat(privatePath)
	trustState, scope, _ := effectiveTrustState(paths, keyID)
	payload := map[string]any{
		"id":          publicRecord.Identifier,
		"purpose":     publicRecord.Purpose,
		"algorithm":   publicRecord.Algorithm,
		"fingerprint": publicRecord.Fingerprint,
		"created_at":  publicRecord.CreatedAt,
		"has_private": privateErr == nil,
		"trust_state": trustState,
		"trust_scope": scope,
	}
	content, _ := encodeJSON(payload)
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func keyExportCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "key export requires a key identifier"}
	}
	keyID := arguments[0]
	purpose := ""
	exportPrivate := false
	confirm := false
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch {
		case argument == "--private":
			exportPrivate = true
		case argument == "--yes":
			confirm = true
		case argument == "--json":
			jsonOutput = true
		case strings.HasPrefix(argument, "--purpose="):
			purpose = strings.TrimPrefix(argument, "--purpose=")
		default:
			return UsageError{Message: fmt.Sprintf("unknown key export option: %s", argument)}
		}
	}
	resolvedPurpose, err := resolveKeyPurpose(paths, keyID, purpose)
	if err != nil {
		return err
	}
	publicRecord, err := loadPublicKeyRecord(paths, resolvedPurpose, keyID)
	if err != nil {
		return err
	}
	payload := map[string]any{"public": publicRecord}
	if exportPrivate {
		if !confirm {
			return securityError{Code: securityCodeKeyExportFailed, Message: "private-key export requires --yes"}
		}
		privateRecord, err := loadPrivateKeyRecord(paths, resolvedPurpose, keyID)
		if err != nil {
			return err
		}
		payload["private"] = privateRecord
	}
	content, err := encodeJSON(payload)
	if err != nil {
		return securityError{Code: securityCodeKeyExportFailed, Message: "encode key export failed"}
	}
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func keyImportCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "key import requires a path"}
	}
	importPath := arguments[0]
	purpose := ""
	privateFlag := false
	keyIDOverride := ""
	unlock := keyUnlockOptions{}
	for i := 1; i < len(arguments); i++ {
		switch arguments[i] {
		case "--purpose":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--purpose requires a value"}
			}
			i++
			purpose = arguments[i]
		case "--private":
			privateFlag = true
		case "--id":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--id requires a value"}
			}
			i++
			keyIDOverride = arguments[i]
		case "--passphrase":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase requires a value"}
			}
			i++
			unlock.PassphraseLiteral = arguments[i]
		case "--passphrase-ref":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase-ref requires a value"}
			}
			i++
			unlock.PassphraseRef = arguments[i]
		default:
			return UsageError{Message: fmt.Sprintf("unknown key import option: %s", arguments[i])}
		}
	}
	content, err := os.ReadFile(importPath)
	if err != nil {
		return securityError{Code: securityCodeKeyImportFailed, Message: fmt.Sprintf("read import file failed: %v", err)}
	}
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal(content, &payload); err != nil {
		return securityError{Code: securityCodeInvalidKeyFormat, Message: "imported key file must be JSON"}
	}
	publicRaw, hasPublic := payload["public"]
	privateRaw, hasPrivate := payload["private"]
	if !hasPublic && !hasPrivate {
		return securityError{Code: securityCodeInvalidKeyFormat, Message: "import file has no key material"}
	}
	if hasPublic {
		record := keyPublicRecord{}
		if err := json.Unmarshal(publicRaw, &record); err != nil {
			return securityError{Code: securityCodeInvalidKeyFormat, Message: "public key record is invalid"}
		}
		if purpose != "" {
			record.Purpose = purpose
		}
		if keyIDOverride != "" {
			record.Identifier = keyIDOverride
		}
		if err := validateKeyIdentifier(record.Identifier); err != nil {
			return err
		}
		if !isSupportedKeyPurpose(record.Purpose) {
			return securityError{Code: securityCodeInvalidKeyPurpose, Message: "public key purpose is invalid"}
		}
		if record.Source == "" {
			record.Source = "imported"
		}
		publicPath, _ := keyPublicPath(paths, record.Purpose, record.Identifier)
		if fileExists(publicPath) {
			return securityError{Code: securityCodeDuplicateKeyIdentifier, Message: fmt.Sprintf("public key %q already exists", record.Identifier)}
		}
		encoded, _ := encodeJSON(record)
		if err := writeFileAtomic(publicPath, encoded, 0o644, false); err != nil {
			return securityError{Code: securityCodeKeyImportFailed, Message: err.Error()}
		}
	}
	if hasPrivate || privateFlag {
		record := keyPrivateRecord{}
		if hasPrivate {
			if err := json.Unmarshal(privateRaw, &record); err != nil {
				return securityError{Code: securityCodeInvalidKeyFormat, Message: "private key record is invalid"}
			}
		} else {
			return securityError{Code: securityCodeInvalidKeyFormat, Message: "private key import requires private record"}
		}
		if purpose != "" {
			record.Purpose = purpose
		}
		if keyIDOverride != "" {
			record.Identifier = keyIDOverride
		}
		if err := validateKeyIdentifier(record.Identifier); err != nil {
			return err
		}
		if !isSupportedKeyPurpose(record.Purpose) {
			return securityError{Code: securityCodeInvalidKeyPurpose, Message: "private key purpose is invalid"}
		}
		if record.PassphraseRef == "" {
			record.PassphraseRef = unlock.PassphraseRef
		}
		privatePath, _ := keyPrivatePath(paths, record.Purpose, record.Identifier)
		if fileExists(privatePath) {
			return securityError{Code: securityCodeDuplicateKeyIdentifier, Message: fmt.Sprintf("private key %q already exists", record.Identifier)}
		}
		encoded, _ := encodeJSON(record)
		if err := writeFileAtomic(privatePath, encoded, 0o600, false); err != nil {
			return securityError{Code: securityCodeKeyImportFailed, Message: err.Error()}
		}
	}
	fmt.Println("key import succeeded")
	return nil
}

func isSupportedKeyPurpose(purpose string) bool {
	return purpose == keyPurposeSigning || purpose == keyPurposeEncryption
}

func keyRemoveCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "key remove requires a key identifier"}
	}
	keyID := arguments[0]
	purpose := ""
	removePublic := false
	removePrivate := false
	confirm := false
	for _, argument := range arguments[1:] {
		switch {
		case argument == "--public":
			removePublic = true
		case argument == "--private":
			removePrivate = true
		case argument == "--yes":
			confirm = true
		case strings.HasPrefix(argument, "--purpose="):
			purpose = strings.TrimPrefix(argument, "--purpose=")
		default:
			return UsageError{Message: fmt.Sprintf("unknown key remove option: %s", argument)}
		}
	}
	if !removePublic && !removePrivate {
		return UsageError{Message: "key remove requires --public and/or --private"}
	}
	if !confirm {
		return securityError{Code: securityCodeKeyRemovalFailed, Message: "key removal requires --yes"}
	}
	resolvedPurpose, err := resolveKeyPurpose(paths, keyID, purpose)
	if err != nil {
		return err
	}
	if removePublic {
		path, _ := keyPublicPath(paths, resolvedPurpose, keyID)
		if fileExists(path) {
			if err := os.Remove(path); err != nil {
				return securityError{Code: securityCodeKeyRemovalFailed, Message: fmt.Sprintf("remove public key %q failed", keyID)}
			}
		}
	}
	if removePrivate {
		path, _ := keyPrivatePath(paths, resolvedPurpose, keyID)
		if fileExists(path) {
			if err := os.Remove(path); err != nil {
				return securityError{Code: securityCodeKeyRemovalFailed, Message: fmt.Sprintf("remove private key %q failed", keyID)}
			}
		}
	}
	fmt.Printf("key removed id=%s purpose=%s\n", keyID, resolvedPurpose)
	return nil
}

func trustCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "trust requires a subcommand"}
	}
	switch arguments[0] {
	case "set":
		return trustSetCommand(paths, arguments[1:])
	case "show":
		return trustShowCommand(paths, arguments[1:])
	case "list":
		return trustListCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown trust subcommand: %s", arguments[0])}
	}
}

func trustSetCommand(paths Paths, arguments []string) error {
	if len(arguments) < 2 {
		return UsageError{Message: "trust set requires <key-id> <state>"}
	}
	keyID := arguments[0]
	state := arguments[1]
	scope := "global"
	supersedes := []string{}
	for i := 2; i < len(arguments); i++ {
		switch arguments[i] {
		case "--scope":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--scope requires a value"}
			}
			i++
			scope = arguments[i]
		case "--supersedes":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--supersedes requires a value"}
			}
			i++
			supersedes = append(supersedes, arguments[i])
		default:
			return UsageError{Message: fmt.Sprintf("unknown trust set option: %s", arguments[i])}
		}
	}
	if err := validateKeyIdentifier(keyID); err != nil {
		return err
	}
	if !validTrustState(state) {
		return securityError{Code: securityCodeInvalidTrustState, Message: fmt.Sprintf("invalid trust state %q", state)}
	}
	file, err := loadTrustFile(paths, scope)
	if err != nil {
		return err
	}
	file.Records[keyID] = trustRecord{State: state, Supersedes: supersedes, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := saveTrustFile(paths, scope, file); err != nil {
		return err
	}
	fmt.Printf("trust updated key=%s state=%s scope=%s\n", keyID, state, scope)
	return nil
}

func trustShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "trust show requires a key identifier"}
	}
	keyID := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown trust show option: %s", argument)}
		}
	}
	state, scope, _ := effectiveTrustState(paths, keyID)
	global, _ := loadTrustFile(paths, "global")
	project, _ := loadTrustFile(paths, "project")
	payload := map[string]any{
		"key_id":          keyID,
		"effective_state": state,
		"effective_scope": scope,
		"global":          global.Records[keyID],
		"project":         project.Records[keyID],
	}
	content, _ := encodeJSON(payload)
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func trustListCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	scope := "effective"
	for i := 0; i < len(arguments); i++ {
		switch arguments[i] {
		case "--json":
			jsonOutput = true
		case "--scope":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--scope requires a value"}
			}
			i++
			scope = arguments[i]
		default:
			return UsageError{Message: fmt.Sprintf("unknown trust list option: %s", arguments[i])}
		}
	}
	payload := map[string]any{}
	switch scope {
	case "global", "project":
		file, err := loadTrustFile(paths, scope)
		if err != nil {
			return err
		}
		payload["scope"] = scope
		payload["records"] = file.Records
	case "effective":
		global, _ := loadTrustFile(paths, "global")
		project, _ := loadTrustFile(paths, "project")
		keys := map[string]bool{}
		for key := range global.Records {
			keys[key] = true
		}
		for key := range project.Records {
			keys[key] = true
		}
		resolved := map[string]map[string]any{}
		for key := range keys {
			state, source, _ := effectiveTrustState(paths, key)
			resolved[key] = map[string]any{"state": state, "scope": source}
		}
		payload["scope"] = scope
		payload["records"] = resolved
	default:
		return UsageError{Message: "trust list scope must be global, project, or effective"}
	}
	content, _ := encodeJSON(payload)
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func recipientPublicKeys(paths Paths, keyIDs []string) ([]keyPublicRecord, error) {
	if len(keyIDs) == 0 {
		return nil, securityError{Code: securityCodeRecipientKeyNotFound, Message: "at least one encryption recipient is required"}
	}
	seen := map[string]bool{}
	records := []keyPublicRecord{}
	for _, keyID := range keyIDs {
		if seen[keyID] {
			continue
		}
		seen[keyID] = true
		record, err := loadPublicKeyRecord(paths, keyPurposeEncryption, keyID)
		if err != nil {
			return nil, securityError{Code: securityCodeRecipientKeyNotFound, Message: fmt.Sprintf("recipient key %q not found", keyID)}
		}
		if record.Algorithm != encryptionAlgorithmX25519GCM {
			return nil, securityError{Code: securityCodeUnsupportedEncryptionAlgorithm, Message: fmt.Sprintf("unsupported recipient algorithm %q", record.Algorithm)}
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Identifier < records[j].Identifier })
	return records, nil
}

func encryptedPayloadForArchive(archive bundleArchive) ([]byte, error) {
	resources := make([]encryptedBundleResourceEntry, 0, len(archive.Resources))
	paths := bundleResourceKeys(archive.Resources)
	sort.Strings(paths)
	for _, resourcePath := range paths {
		resources = append(resources, encryptedBundleResourceEntry{Path: resourcePath, Content: base64.StdEncoding.EncodeToString(archive.Resources[resourcePath])})
	}
	payload := encryptedBundlePayload{Manifest: archive.Manifest, Resources: resources}
	return json.Marshal(payload)
}

func restoreArchiveFromEncryptedPayload(content []byte) (bundleArchive, error) {
	payload := encryptedBundlePayload{}
	if err := json.Unmarshal(content, &payload); err != nil {
		return bundleArchive{}, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "encrypted payload is invalid"}
	}
	resources := map[string][]byte{}
	for _, resource := range payload.Resources {
		decoded, err := base64.StdEncoding.DecodeString(resource.Content)
		if err != nil {
			return bundleArchive{}, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: fmt.Sprintf("invalid payload resource %s", resource.Path)}
		}
		resources[resource.Path] = decoded
	}
	return bundleArchive{Manifest: payload.Manifest, Resources: resources}, nil
}

func encryptArchiveForRecipients(paths Paths, archive bundleArchive, recipientIDs []string) (bundleArchive, error) {
	recipients, err := recipientPublicKeys(paths, recipientIDs)
	if err != nil {
		return bundleArchive{}, err
	}
	plaintext, err := encryptedPayloadForArchive(archive)
	if err != nil {
		return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "serialize bundle payload failed"}
	}
	contentKey := make([]byte, 32)
	if _, err := rand.Read(contentKey); err != nil {
		return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "generate content key failed"}
	}
	payloadNonce := make([]byte, 12)
	if _, err := rand.Read(payloadNonce); err != nil {
		return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "generate payload nonce failed"}
	}
	payloadBlock, err := aes.NewCipher(contentKey)
	if err != nil {
		return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "initialize payload cipher failed"}
	}
	payloadAEAD, err := cipher.NewGCM(payloadBlock)
	if err != nil {
		return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "initialize payload AEAD failed"}
	}
	aad := []byte("ai-dev-bundle-encryption-v1")
	ciphertext := payloadAEAD.Seal(nil, payloadNonce, plaintext, aad)
	recipientEntries := make([]bundleRecipient, 0, len(recipients))
	curve := ecdh.X25519()
	for _, recipient := range recipients {
		recipientPublicBytes, err := base64.StdEncoding.DecodeString(recipient.PublicKey)
		if err != nil {
			return bundleArchive{}, securityError{Code: securityCodeInvalidKeyFormat, Message: fmt.Sprintf("invalid recipient public key %q", recipient.Identifier)}
		}
		recipientPublic, err := curve.NewPublicKey(recipientPublicBytes)
		if err != nil {
			return bundleArchive{}, securityError{Code: securityCodeInvalidKeyFormat, Message: fmt.Sprintf("invalid recipient public key %q", recipient.Identifier)}
		}
		ephemeralPrivate, err := curve.GenerateKey(rand.Reader)
		if err != nil {
			return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "generate ephemeral key failed"}
		}
		sharedSecret, err := ephemeralPrivate.ECDH(recipientPublic)
		if err != nil {
			return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "derive recipient wrapping secret failed"}
		}
		wrapKeyMaterial := sha256.Sum256(append(sharedSecret, []byte("ai-dev-wrap-v1")...))
		wrapBlock, err := aes.NewCipher(wrapKeyMaterial[:])
		if err != nil {
			return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "initialize wrapping cipher failed"}
		}
		wrapAEAD, err := cipher.NewGCM(wrapBlock)
		if err != nil {
			return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "initialize wrapping AEAD failed"}
		}
		wrapNonce := make([]byte, 12)
		if _, err := rand.Read(wrapNonce); err != nil {
			return bundleArchive{}, securityError{Code: securityCodeEncryptionFailed, Message: "generate wrapping nonce failed"}
		}
		wrapped := wrapAEAD.Seal(nil, wrapNonce, contentKey, []byte(recipient.Fingerprint))
		recipientEntries = append(recipientEntries, bundleRecipient{
			KeyID:           recipient.Identifier,
			Fingerprint:     recipient.Fingerprint,
			EphemeralKey:    base64.StdEncoding.EncodeToString(ephemeralPrivate.PublicKey().Bytes()),
			WrappedKey:      base64.StdEncoding.EncodeToString(wrapped),
			WrappedKeyNonce: base64.StdEncoding.EncodeToString(wrapNonce),
		})
	}
	sort.Slice(recipientEntries, func(i, j int) bool { return recipientEntries[i].Fingerprint < recipientEntries[j].Fingerprint })
	payloadDigest := checksumForContent(ciphertext)
	security := &bundleSecurityEnvelope{
		Version:   securityEnvelopeV1,
		Encrypted: true,
		Encryption: &bundleEncryptionDetails{
			Algorithm:      encryptionAlgorithmX25519GCM,
			PayloadNonce:   base64.StdEncoding.EncodeToString(payloadNonce),
			PayloadDigest:  payloadDigest,
			Recipients:     recipientEntries,
			Authenticated:  map[string]string{"bundle_schema": bundleSchemaV1},
			SecurityFormat: securityEnvelopeV1,
		},
	}
	return bundleArchive{Manifest: bundleManifest{}, Resources: map[string][]byte{}, Security: security, EncryptedPayload: ciphertext}, nil
}

func decryptArchive(paths Paths, archive bundleArchive, keyID string, unlock keyUnlockOptions) (bundleArchive, string, error) {
	if archive.Security == nil || !archive.Security.Encrypted || archive.Security.Encryption == nil {
		return archive, "", securityError{Code: securityCodeEncryptedBundleNeedsDecrypt, Message: "bundle is not encrypted"}
	}
	if archive.Security.Encryption.Algorithm != encryptionAlgorithmX25519GCM {
		return bundleArchive{}, "", securityError{Code: securityCodeUnsupportedEncryptionAlgorithm, Message: fmt.Sprintf("unsupported encryption algorithm %q", archive.Security.Encryption.Algorithm)}
	}
	curve := ecdh.X25519()
	recipients := archive.Security.Encryption.Recipients
	if len(recipients) == 0 {
		return bundleArchive{}, "", securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "encrypted bundle has no recipients"}
	}
	selected := []bundleRecipient{}
	for _, recipient := range recipients {
		if keyID == "" || recipient.KeyID == keyID {
			selected = append(selected, recipient)
		}
	}
	if len(selected) == 0 {
		return bundleArchive{}, "", securityError{Code: securityCodeRecipientKeyNotFound, Message: "no matching recipient key"}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].KeyID < selected[j].KeyID })
	var contentKey []byte
	usedKeyID := ""
	for _, recipient := range selected {
		candidateID := recipient.KeyID
		if candidateID == "" {
			candidateID = keyID
		}
		privateRecord, err := loadPrivateKeyRecord(paths, keyPurposeEncryption, candidateID)
		if err != nil {
			continue
		}
		privateBytes, err := decryptPrivateKeyMaterial(paths, privateRecord, unlock)
		if err != nil {
			continue
		}
		privateKey, err := curve.NewPrivateKey(privateBytes)
		if err != nil {
			continue
		}
		ephemeralBytes, err := base64.StdEncoding.DecodeString(recipient.EphemeralKey)
		if err != nil {
			continue
		}
		ephemeralPublic, err := curve.NewPublicKey(ephemeralBytes)
		if err != nil {
			continue
		}
		sharedSecret, err := privateKey.ECDH(ephemeralPublic)
		if err != nil {
			continue
		}
		wrapKeyMaterial := sha256.Sum256(append(sharedSecret, []byte("ai-dev-wrap-v1")...))
		wrapBlock, err := aes.NewCipher(wrapKeyMaterial[:])
		if err != nil {
			continue
		}
		wrapAEAD, err := cipher.NewGCM(wrapBlock)
		if err != nil {
			continue
		}
		wrapNonce, err := base64.StdEncoding.DecodeString(recipient.WrappedKeyNonce)
		if err != nil {
			continue
		}
		wrapped, err := base64.StdEncoding.DecodeString(recipient.WrappedKey)
		if err != nil {
			continue
		}
		opened, err := wrapAEAD.Open(nil, wrapNonce, wrapped, []byte(recipient.Fingerprint))
		if err != nil {
			continue
		}
		contentKey = opened
		usedKeyID = candidateID
		break
	}
	if len(contentKey) == 0 {
		return bundleArchive{}, "", securityError{Code: securityCodeRecipientPrivateKeyUnavailable, Message: "unable to decrypt with available recipient private keys"}
	}
	payloadNonce, err := base64.StdEncoding.DecodeString(archive.Security.Encryption.PayloadNonce)
	if err != nil {
		return bundleArchive{}, "", securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "invalid payload nonce"}
	}
	if checksumForContent(archive.EncryptedPayload) != archive.Security.Encryption.PayloadDigest {
		return bundleArchive{}, "", securityError{Code: securityCodeDecryptionFailed, Message: "encrypted payload digest mismatch"}
	}
	payloadBlock, err := aes.NewCipher(contentKey)
	if err != nil {
		return bundleArchive{}, "", securityError{Code: securityCodeDecryptionFailed, Message: "initialize payload cipher failed"}
	}
	payloadAEAD, err := cipher.NewGCM(payloadBlock)
	if err != nil {
		return bundleArchive{}, "", securityError{Code: securityCodeDecryptionFailed, Message: "initialize payload AEAD failed"}
	}
	plaintext, err := payloadAEAD.Open(nil, payloadNonce, archive.EncryptedPayload, []byte("ai-dev-bundle-encryption-v1"))
	if err != nil {
		return bundleArchive{}, "", securityError{Code: securityCodeDecryptionFailed, Message: "decrypt bundle payload failed"}
	}
	decryptedArchive, err := restoreArchiveFromEncryptedPayload(plaintext)
	if err != nil {
		return bundleArchive{}, "", err
	}
	decryptedArchive.Security = archive.Security
	decryptedArchive.EncryptedPayload = archive.EncryptedPayload
	return decryptedArchive, usedKeyID, nil
}

func cloneSecurityWithoutSignatures(security *bundleSecurityEnvelope) *bundleSecurityEnvelope {
	if security == nil {
		return &bundleSecurityEnvelope{Version: securityEnvelopeV1}
	}
	copy := *security
	copy.Signatures = nil
	if security.Encryption != nil {
		encryptionCopy := *security.Encryption
		encryptionCopy.Recipients = append([]bundleRecipient{}, security.Encryption.Recipients...)
		copy.Encryption = &encryptionCopy
	}
	return &copy
}

func canonicalSignedPayload(archive bundleArchive, security *bundleSecurityEnvelope) ([]byte, error) {
	securityCopy := cloneSecurityWithoutSignatures(security)
	manifestChecksums := []map[string]string{}
	checksumKeys := mapKeysString(archive.Manifest.Checksums)
	for _, key := range checksumKeys {
		manifestChecksums = append(manifestChecksums, map[string]string{"path": key, "checksum": archive.Manifest.Checksums[key]})
	}
	resourcePaths := bundleResourceKeys(archive.Resources)
	sort.Strings(resourcePaths)
	resources := make([]map[string]string, 0, len(resourcePaths))
	for _, resourcePath := range resourcePaths {
		resources = append(resources, map[string]string{
			"path":    resourcePath,
			"content": base64.StdEncoding.EncodeToString(archive.Resources[resourcePath]),
			"digest":  checksumForContent(archive.Resources[resourcePath]),
		})
	}
	payload := map[string]any{
		"security_version": securityCopy.Version,
		"encrypted":        securityCopy.Encrypted,
		"encryption":       securityCopy.Encryption,
		"bundle_schema":    archive.Manifest.Schema,
		"manifest": map[string]any{
			"schema":             archive.Manifest.Schema,
			"bundle_version":     archive.Manifest.BundleVersion,
			"created_at":         archive.Manifest.CreatedAt,
			"creator_version":    archive.Manifest.CreatorVersion,
			"origin_platform":    archive.Manifest.OriginPlatform,
			"project_identifier": archive.Manifest.ProjectIdentifier,
			"resources":          archive.Manifest.Resources,
			"checksums":          manifestChecksums,
		},
		"resources": resources,
	}
	if securityCopy.Encrypted {
		payload["encrypted_payload_digest"] = checksumForContent(archive.EncryptedPayload)
	}
	return json.Marshal(payload)
}

func mapKeysString(input map[string]string) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func loadSigningPrivateKey(paths Paths, keyID string, unlock keyUnlockOptions) (ed25519.PrivateKey, keyPublicRecord, error) {
	publicRecord, err := loadPublicKeyRecord(paths, keyPurposeSigning, keyID)
	if err != nil {
		return nil, keyPublicRecord{}, err
	}
	if publicRecord.Algorithm != signingAlgorithmEd25519 {
		return nil, keyPublicRecord{}, securityError{Code: securityCodeUnsupportedSignatureAlgorithm, Message: fmt.Sprintf("unsupported signing algorithm %q", publicRecord.Algorithm)}
	}
	privateRecord, err := loadPrivateKeyRecord(paths, keyPurposeSigning, keyID)
	if err != nil {
		return nil, keyPublicRecord{}, err
	}
	privateBytes, err := decryptPrivateKeyMaterial(paths, privateRecord, unlock)
	if err != nil {
		return nil, keyPublicRecord{}, err
	}
	return ed25519.PrivateKey(privateBytes), publicRecord, nil
}

func signBundleArchive(paths Paths, archive *bundleArchive, keyID string, unlock keyUnlockOptions) error {
	privateKey, publicRecord, err := loadSigningPrivateKey(paths, keyID, unlock)
	if err != nil {
		return err
	}
	if archive.Security == nil {
		archive.Security = &bundleSecurityEnvelope{Version: securityEnvelopeV1}
	}
	if archive.Security.Version == "" {
		archive.Security.Version = securityEnvelopeV1
	}
	if archive.Security.Version != securityEnvelopeV1 {
		return securityError{Code: securityCodeUnsupportedSecurityEnvelope, Message: fmt.Sprintf("unsupported security envelope version %q", archive.Security.Version)}
	}
	canonical, err := canonicalSignedPayload(*archive, archive.Security)
	if err != nil {
		return securityError{Code: securityCodeSigningFailed, Message: "failed to create canonical signing payload"}
	}
	digest := sha256.Sum256(canonical)
	signature := ed25519.Sign(privateKey, digest[:])
	archive.Security.Signatures = append(archive.Security.Signatures, bundleSignature{
		Algorithm:         signingAlgorithmEd25519,
		SignerKeyID:       keyID,
		SignerFingerprint: publicRecord.Fingerprint,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		SecurityVersion:   securityEnvelopeV1,
		CoveredDigest:     hex.EncodeToString(digest[:]),
		Signature:         base64.StdEncoding.EncodeToString(signature),
	})
	return nil
}

func verifyBundleSignatures(paths Paths, archive bundleArchive) ([]signatureVerificationResult, error) {
	if archive.Security == nil || len(archive.Security.Signatures) == 0 {
		return []signatureVerificationResult{}, nil
	}
	if archive.Security.Version != securityEnvelopeV1 {
		return nil, securityError{Code: securityCodeUnsupportedSecurityEnvelope, Message: fmt.Sprintf("unsupported security envelope version %q", archive.Security.Version)}
	}
	canonical, err := canonicalSignedPayload(archive, archive.Security)
	if err != nil {
		return nil, securityError{Code: securityCodeSignatureInvalid, Message: "failed to build canonical signing payload"}
	}
	digest := sha256.Sum256(canonical)
	expectedDigest := hex.EncodeToString(digest[:])
	results := make([]signatureVerificationResult, 0, len(archive.Security.Signatures))
	for _, signatureEntry := range archive.Security.Signatures {
		result := signatureVerificationResult{
			SignerKeyID:       signatureEntry.SignerKeyID,
			SignerFingerprint: signatureEntry.SignerFingerprint,
			Algorithm:         signatureEntry.Algorithm,
			Timestamp:         signatureEntry.CreatedAt,
			CoveredDigest:     signatureEntry.CoveredDigest,
			Valid:             false,
			TrustState:        trustStateUnknown,
		}
		if signatureEntry.Algorithm != signingAlgorithmEd25519 {
			result.Code = securityCodeUnsupportedSignatureAlgorithm
			result.Message = "unsupported signature algorithm"
			results = append(results, result)
			continue
		}
		if signatureEntry.CoveredDigest != expectedDigest {
			result.Code = securityCodeSignatureInvalid
			result.Message = "signature digest does not match covered payload"
			results = append(results, result)
			continue
		}
		publicRecord, err := loadPublicKeyRecord(paths, keyPurposeSigning, signatureEntry.SignerKeyID)
		if err != nil {
			result.Code = securityCodeKeyNotFound
			result.Message = "signer public key is unavailable"
			results = append(results, result)
			continue
		}
		publicBytes, err := base64.StdEncoding.DecodeString(publicRecord.PublicKey)
		if err != nil {
			result.Code = securityCodeInvalidKeyFormat
			result.Message = "signer public key encoding is invalid"
			results = append(results, result)
			continue
		}
		signatureBytes, err := base64.StdEncoding.DecodeString(signatureEntry.Signature)
		if err != nil {
			result.Code = securityCodeSignatureInvalid
			result.Message = "signature encoding is invalid"
			results = append(results, result)
			continue
		}
		if !ed25519.Verify(ed25519.PublicKey(publicBytes), digest[:], signatureBytes) {
			result.Code = securityCodeSignatureInvalid
			result.Message = "signature verification failed"
			results = append(results, result)
			continue
		}
		result.Valid = true
		trustState, _, _ := effectiveTrustState(paths, signatureEntry.SignerKeyID)
		result.TrustState = trustState
		if trustState == trustStateRevoked {
			result.Code = securityCodeSignerRevoked
			result.Message = "signature is valid but signer is revoked"
		}
		results = append(results, result)
	}
	return results, nil
}

func verifyBundleSecurity(paths Paths, archive bundleArchive) (bundleSecurityVerification, error) {
	verification := bundleSecurityVerification{Signed: false, Encrypted: false, Signatures: []signatureVerificationResult{}}
	if archive.Security == nil {
		return verification, nil
	}
	if archive.Security.Version != securityEnvelopeV1 {
		return verification, securityError{Code: securityCodeUnsupportedSecurityEnvelope, Message: fmt.Sprintf("unsupported security envelope version %q", archive.Security.Version)}
	}
	verification.EnvelopeVersion = archive.Security.Version
	verification.Encrypted = archive.Security.Encrypted
	if archive.Security.Encrypted {
		if archive.Security.Encryption == nil {
			return verification, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "missing encryption metadata"}
		}
		if archive.Security.Encryption.Algorithm != encryptionAlgorithmX25519GCM {
			return verification, securityError{Code: securityCodeUnsupportedEncryptionAlgorithm, Message: fmt.Sprintf("unsupported encryption algorithm %q", archive.Security.Encryption.Algorithm)}
		}
		fingerprints := []string{}
		seen := map[string]bool{}
		for _, recipient := range archive.Security.Encryption.Recipients {
			if recipient.Fingerprint == "" {
				return verification, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "recipient fingerprint is missing"}
			}
			if seen[recipient.Fingerprint] {
				return verification, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "duplicate recipient fingerprint"}
			}
			seen[recipient.Fingerprint] = true
			fingerprints = append(fingerprints, recipient.Fingerprint)
		}
		sort.Strings(fingerprints)
		verification.RecipientFingerprints = fingerprints
		if checksumForContent(archive.EncryptedPayload) != archive.Security.Encryption.PayloadDigest {
			return verification, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "encrypted payload digest mismatch"}
		}
	}
	if len(archive.Security.Signatures) > 0 {
		verification.Signed = true
		results, err := verifyBundleSignatures(paths, archive)
		if err != nil {
			return verification, err
		}
		verification.Signatures = results
		for _, result := range results {
			if result.Valid {
				verification.HasAnyValid = true
				if result.TrustState == trustStateTrusted {
					verification.HasTrustedValid = true
				}
			}
		}
	}
	return verification, nil
}

func enforceVerifyRequirements(verification bundleSecurityVerification, requireTrusted bool, requiredSigners []string) error {
	if requireTrusted && !verification.HasTrustedValid {
		return securityError{Code: securityCodeSignatureUntrusted, Message: "no valid trusted signature found"}
	}
	if len(requiredSigners) == 0 {
		return nil
	}
	needed := map[string]bool{}
	for _, signer := range requiredSigners {
		needed[signer] = false
	}
	for _, result := range verification.Signatures {
		if _, ok := needed[result.SignerKeyID]; !ok {
			continue
		}
		if result.Valid && result.TrustState != trustStateRevoked && result.TrustState == trustStateTrusted {
			needed[result.SignerKeyID] = true
		}
	}
	missing := []string{}
	for signer, ok := range needed {
		if !ok {
			missing = append(missing, signer)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return securityError{Code: securityCodeRequiredSignerMissing, Message: fmt.Sprintf("required signer(s) missing or untrusted: %s", strings.Join(missing, ","))}
	}
	return nil
}

func evaluateImportPolicy(paths Paths, archive bundleArchive, policy importTrustPolicy) (bundleSecurityVerification, error) {
	verification, err := verifyBundleSecurity(paths, archive)
	if err != nil {
		return verification, err
	}
	mode := policy.Mode
	if mode == "" {
		mode = "allow-unsigned"
	}
	switch mode {
	case "allow-unsigned":
		return verification, nil
	case "require-signed":
		if !verification.Signed || !verification.HasAnyValid {
			return verification, securityError{Code: securityCodeSignatureMissing, Message: "import policy requires a valid signature"}
		}
	case "require-trusted":
		if !verification.HasTrustedValid {
			return verification, securityError{Code: securityCodeSignatureUntrusted, Message: "import policy requires a trusted non-revoked signature"}
		}
	case "require-specific-signers":
		if len(policy.RequiredSigners) == 0 {
			return verification, securityError{Code: securityCodeBundlePolicyFailed, Message: "require-specific-signers policy needs configured signers"}
		}
		if err := enforceVerifyRequirements(verification, false, policy.RequiredSigners); err != nil {
			return verification, err
		}
	default:
		return verification, securityError{Code: securityCodeBundlePolicyFailed, Message: fmt.Sprintf("unknown import policy %q", mode)}
	}
	return verification, nil
}

func parsePolicyStrictness(policy string) int {
	switch policy {
	case "allow-unsigned":
		return 0
	case "require-signed":
		return 1
	case "require-trusted":
		return 2
	case "require-specific-signers":
		return 3
	default:
		return -1
	}
}

func mergeImportPolicy(configured importTrustPolicy, requested importTrustPolicy) (importTrustPolicy, error) {
	if configured.Mode == "" {
		configured.Mode = "allow-unsigned"
	}
	if requested.Mode == "" {
		requested.Mode = configured.Mode
	}
	configuredRank := parsePolicyStrictness(configured.Mode)
	requestedRank := parsePolicyStrictness(requested.Mode)
	if configuredRank < 0 || requestedRank < 0 {
		return importTrustPolicy{}, securityError{Code: securityCodeBundlePolicyFailed, Message: "unknown bundle security policy"}
	}
	if requestedRank < configuredRank {
		return importTrustPolicy{}, securityError{Code: securityCodeBundlePolicyFailed, Message: "command-line policy cannot weaken configured policy"}
	}
	finalPolicy := configured
	if requestedRank > configuredRank {
		finalPolicy.Mode = requested.Mode
	}
	if len(requested.RequiredSigners) > 0 {
		finalPolicy.Mode = "require-specific-signers"
		finalPolicy.RequiredSigners = append([]string{}, requested.RequiredSigners...)
	}
	return finalPolicy, nil
}

func loadConfiguredImportPolicy(paths Paths) (importTrustPolicy, error) {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return importTrustPolicy{Mode: "allow-unsigned"}, nil
	}
	resolved, _, err := resolveConfiguration(paths, info)
	if err != nil {
		return importTrustPolicy{Mode: "allow-unsigned"}, nil
	}
	bundlesValue, ok := resolved["bundles"].(map[string]any)
	if !ok {
		return importTrustPolicy{Mode: "allow-unsigned"}, nil
	}
	securityValue, ok := bundlesValue["security"].(map[string]any)
	if !ok {
		return importTrustPolicy{Mode: "allow-unsigned"}, nil
	}
	policy := importTrustPolicy{Mode: "allow-unsigned", RequiredSigners: []string{}}
	if mode, ok := securityValue["import_policy"].(string); ok && mode != "" {
		policy.Mode = mode
	}
	if signers, ok := securityValue["required_signers"].([]any); ok {
		for _, signer := range signers {
			signerID, ok := signer.(string)
			if !ok {
				continue
			}
			policy.RequiredSigners = append(policy.RequiredSigners, signerID)
		}
	}
	return policy, nil
}

func keyRegistryDoctorLines(paths Paths) []string {
	lines := []string{}
	registry := keyRegistryRoot(paths)
	if err := os.MkdirAll(registry, 0o755); err != nil {
		lines = append(lines, fmt.Sprintf("[error] key registry: code=%s message=unavailable", securityCodeKeyImportFailed))
		return lines
	}
	lines = append(lines, fmt.Sprintf("[ok] key registry: %s", registry))
	entries, err := listAllKeyEntries(paths)
	if err != nil {
		lines = append(lines, fmt.Sprintf("[error] key registry scan: %v", err))
		return lines
	}
	lines = append(lines, fmt.Sprintf("[ok] key summary: total=%d", len(entries)))
	for _, entry := range entries {
		if entry.HasPrivate {
			privatePath, _ := keyPrivatePath(paths, entry.Purpose, entry.Identifier)
			if info, err := os.Stat(privatePath); err == nil {
				if info.Mode().Perm()&0o077 != 0 {
					lines = append(lines, fmt.Sprintf("[error] private key permission: code=%s key=%s path=%s", securityCodePrivateKeyPermissionUnsafe, entry.Identifier, privatePath))
				}
			}
		}
	}
	policy, _ := loadConfiguredImportPolicy(paths)
	lines = append(lines, fmt.Sprintf("[ok] bundle security policy: import_policy=%s required_signers=%d", policy.Mode, len(policy.RequiredSigners)))
	for _, signer := range policy.RequiredSigners {
		state, _, _ := effectiveTrustState(paths, signer)
		if state == trustStateRevoked {
			lines = append(lines, fmt.Sprintf("[error] revoked required signer: code=%s signer=%s", securityCodeSignerRevoked, signer))
		}
	}
	return lines
}

func serializeSecurityEnvelope(envelope *bundleSecurityEnvelope) ([]byte, error) {
	if envelope == nil {
		return nil, nil
	}
	return json.MarshalIndent(envelope, "", "  ")
}

func writeBundleSecurityProvenance(paths Paths, archive bundleArchive, verification bundleSecurityVerification) error {
	filePath := filepath.Join(paths.StateHome, "bundle-security-provenance.json")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	validFingerprints := []string{}
	for _, signature := range verification.Signatures {
		if signature.Valid {
			validFingerprints = append(validFingerprints, signature.SignerFingerprint)
		}
	}
	sort.Strings(validFingerprints)
	payload := map[string]any{
		"bundle_schema":             archive.Manifest.Schema,
		"encrypted":                 verification.Encrypted,
		"valid_signer_fingerprints": validFingerprints,
		"effective_trust_result":    verification.HasTrustedValid,
		"verified_at":               time.Now().UTC().Format(time.RFC3339),
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, content, 0o600)
}

func parseSecurityEnvelope(content []byte) (*bundleSecurityEnvelope, error) {
	envelope := bundleSecurityEnvelope{}
	if err := json.Unmarshal(content, &envelope); err != nil {
		return nil, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "security envelope is invalid JSON"}
	}
	if envelope.Version == "" {
		return nil, securityError{Code: securityCodeInvalidSecurityEnvelope, Message: "security envelope version is missing"}
	}
	if envelope.Version != securityEnvelopeV1 {
		return nil, securityError{Code: securityCodeUnsupportedSecurityEnvelope, Message: fmt.Sprintf("unsupported security envelope version %q", envelope.Version)}
	}
	return &envelope, nil
}

func bundleSignCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle sign requires a bundle path"}
	}
	bundlePath := arguments[0]
	keyID := ""
	outputPath := ""
	unlock := keyUnlockOptions{}
	for i := 1; i < len(arguments); i++ {
		switch arguments[i] {
		case "--key":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--key requires a value"}
			}
			i++
			keyID = arguments[i]
		case "--output":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--output requires a value"}
			}
			i++
			outputPath = arguments[i]
		case "--passphrase":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase requires a value"}
			}
			i++
			unlock.PassphraseLiteral = arguments[i]
		case "--passphrase-ref":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase-ref requires a value"}
			}
			i++
			unlock.PassphraseRef = arguments[i]
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle sign option: %s", arguments[i])}
		}
	}
	if keyID == "" {
		return UsageError{Message: "bundle sign requires --key <key-id>"}
	}
	archive, err := readBundleArchive(bundlePath)
	if err != nil {
		return err
	}
	if archive.Security == nil {
		archive.Security = &bundleSecurityEnvelope{Version: securityEnvelopeV1}
	}
	if err := signBundleArchive(paths, &archive, keyID, unlock); err != nil {
		return err
	}
	if outputPath == "" {
		outputPath = bundlePath
	}
	if err := writeBundleArchive(outputPath, archive); err != nil {
		return err
	}
	fmt.Printf("bundle signed path=%s signer=%s\n", outputPath, keyID)
	return nil
}

func bundleVerifySignatureCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle verify-signature requires a bundle path"}
	}
	bundlePath := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle verify-signature option: %s", argument)}
		}
	}
	archive, err := readBundleArchive(bundlePath)
	if err != nil {
		return err
	}
	verification, err := verifyBundleSecurity(paths, archive)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"bundle":            bundlePath,
		"signed":            verification.Signed,
		"encrypted":         verification.Encrypted,
		"envelope_version":  verification.EnvelopeVersion,
		"has_any_valid":     verification.HasAnyValid,
		"has_trusted_valid": verification.HasTrustedValid,
		"signatures":        verification.Signatures,
	}
	content, _ := encodeJSON(payload)
	if jsonOutput {
		fmt.Println(string(content))
	} else {
		fmt.Println(string(content))
	}
	if verification.Signed {
		for _, signature := range verification.Signatures {
			if !signature.Valid {
				return securityError{Code: securityCodeSignatureInvalid, Message: "one or more signatures are invalid"}
			}
		}
	}
	return nil
}

func bundleSignaturesCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle signatures requires a bundle path"}
	}
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle signatures option: %s", argument)}
		}
	}
	archive, err := readBundleArchive(arguments[0])
	if err != nil {
		return err
	}
	verification, err := verifyBundleSecurity(paths, archive)
	if err != nil {
		return err
	}
	content, _ := encodeJSON(map[string]any{"signatures": verification.Signatures})
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func bundleRecipientsCommand(arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle recipients requires a bundle path"}
	}
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle recipients option: %s", argument)}
		}
	}
	archive, err := readBundleArchive(arguments[0])
	if err != nil {
		return err
	}
	recipients := []bundleRecipient{}
	if archive.Security != nil && archive.Security.Encryption != nil {
		recipients = archive.Security.Encryption.Recipients
	}
	content, _ := encodeJSON(map[string]any{"recipients": recipients})
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func bundleDecryptCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle decrypt requires a bundle path"}
	}
	bundlePath := arguments[0]
	outputPath := ""
	selectedKeyID := ""
	unlock := keyUnlockOptions{}
	jsonOutput := false
	for i := 1; i < len(arguments); i++ {
		switch arguments[i] {
		case "--output":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--output requires a value"}
			}
			i++
			outputPath = arguments[i]
		case "--key":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--key requires a value"}
			}
			i++
			selectedKeyID = arguments[i]
		case "--passphrase":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase requires a value"}
			}
			i++
			unlock.PassphraseLiteral = arguments[i]
		case "--passphrase-ref":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase-ref requires a value"}
			}
			i++
			unlock.PassphraseRef = arguments[i]
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle decrypt option: %s", arguments[i])}
		}
	}
	archive, err := readBundleArchive(bundlePath)
	if err != nil {
		return err
	}
	decrypted, keyUsed, err := decryptArchive(paths, archive, selectedKeyID, unlock)
	if err != nil {
		return err
	}
	if outputPath != "" {
		decrypted.Security = nil
		decrypted.EncryptedPayload = nil
		if err := writeBundleArchive(outputPath, decrypted); err != nil {
			return err
		}
		fmt.Printf("bundle decrypted output=%s key=%s\n", outputPath, keyUsed)
		return nil
	}
	payload := map[string]any{"decrypted": true, "key": keyUsed, "resource_count": len(decrypted.Manifest.Resources), "schema": decrypted.Manifest.Schema}
	content, _ := encodeJSON(payload)
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func bundleReencryptCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle reencrypt requires a bundle path"}
	}
	bundlePath := arguments[0]
	addRecipients := []string{}
	removeRecipients := []string{}
	outputPath := ""
	keyID := ""
	unlock := keyUnlockOptions{}
	for i := 1; i < len(arguments); i++ {
		switch arguments[i] {
		case "--add-recipient":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--add-recipient requires a value"}
			}
			i++
			addRecipients = append(addRecipients, arguments[i])
		case "--remove-recipient":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--remove-recipient requires a value"}
			}
			i++
			removeRecipients = append(removeRecipients, arguments[i])
		case "--output":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--output requires a value"}
			}
			i++
			outputPath = arguments[i]
		case "--key":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--key requires a value"}
			}
			i++
			keyID = arguments[i]
		case "--passphrase":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase requires a value"}
			}
			i++
			unlock.PassphraseLiteral = arguments[i]
		case "--passphrase-ref":
			if i+1 >= len(arguments) {
				return UsageError{Message: "--passphrase-ref requires a value"}
			}
			i++
			unlock.PassphraseRef = arguments[i]
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle reencrypt option: %s", arguments[i])}
		}
	}
	archive, err := readBundleArchive(bundlePath)
	if err != nil {
		return err
	}
	decrypted, _, err := decryptArchive(paths, archive, keyID, unlock)
	if err != nil {
		return err
	}
	currentRecipients := map[string]bool{}
	if archive.Security != nil && archive.Security.Encryption != nil {
		for _, recipient := range archive.Security.Encryption.Recipients {
			if recipient.KeyID != "" {
				currentRecipients[recipient.KeyID] = true
			}
		}
	}
	for _, removeID := range removeRecipients {
		delete(currentRecipients, removeID)
	}
	for _, addID := range addRecipients {
		currentRecipients[addID] = true
	}
	nextRecipients := []string{}
	for key := range currentRecipients {
		nextRecipients = append(nextRecipients, key)
	}
	sort.Strings(nextRecipients)
	if len(nextRecipients) == 0 {
		return securityError{Code: securityCodeBundleReencryptFailed, Message: "re-encryption requires at least one recipient"}
	}
	reencrypted, err := encryptArchiveForRecipients(paths, decrypted, nextRecipients)
	if err != nil {
		return err
	}
	if archive.Security != nil && len(archive.Security.Signatures) > 0 {
		reencrypted.Security.Signatures = []bundleSignature{}
	}
	if outputPath == "" {
		outputPath = bundlePath
	}
	if err := writeBundleArchive(outputPath, reencrypted); err != nil {
		return err
	}
	fmt.Printf("bundle reencrypted path=%s recipients=%d\n", outputPath, len(nextRecipients))
	return nil
}
