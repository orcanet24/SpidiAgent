package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost = 12

	// Argon2 params para derivar clave de cifrado desde master key
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

var ErrDecrypt = errors.New("crypto: decryption failed")

// HashPassword genera un hash bcrypt de una contraseña.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword compara un hash bcrypt con una contraseña.
func VerifyPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ConstantTimeCompare comparación resistente a timing attacks.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// deriveKey deriva una clave AES-256 desde un master password usando Argon2.
func deriveKey(masterPassword string, salt []byte) []byte {
	return argon2.IDKey([]byte(masterPassword), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

// Encrypt cifra un texto usando AES-256-GCM con clave derivada del master.
// Retorna: base64(salt + nonce + ciphertext).
func Encrypt(plaintext, masterPassword string) (string, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", err
	}

	key := deriveKey(masterPassword, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Prepend salt
	full := append(salt, ciphertext...)

	return base64.StdEncoding.EncodeToString(full), nil
}

// Decrypt descifra un texto cifrado con Encrypt.
func Decrypt(encodedCiphertext, masterPassword string) (string, error) {
	full, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return "", ErrDecrypt
	}

	if len(full) < 16 {
		return "", ErrDecrypt
	}

	salt := full[:16]
	ciphertext := full[16:]

	key := deriveKey(masterPassword, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", ErrDecrypt
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ErrDecrypt
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", ErrDecrypt
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecrypt
	}

	return string(plaintext), nil
}

// GenerateRandomBytes genera n bytes aleatorios codificados en base64.
func GenerateRandomBytes(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
