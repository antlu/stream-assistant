package crypto

import (
	"encoding/hex"

	"github.com/gtank/cryptopasta"
)

type Cipher string

func (c Cipher) getSecureKey() (*[32]byte, error) {
secureKey, err := hex.DecodeString(string(c))
	if err != nil {
		return nil, err
	}

	return (*[32]byte)(secureKey), nil
}

func (c Cipher) Encrypt(value string) (string, error) {
	secureKey, err := c.getSecureKey()
	if err != nil {
		return "", err
	}

	encryptedValue, err := cryptopasta.Encrypt([]byte(value), secureKey)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(encryptedValue), nil
}

func (c Cipher) Decrypt(value string) (string, error) {
	secureKey, err := c.getSecureKey()
	if err != nil {
		return "", err
	}

	decodedValue, err := hex.DecodeString(value)
	if err != nil {
		return "", err
	}

	decryptedValue, err := cryptopasta.Decrypt(decodedValue, secureKey)
	if err != nil {
		return "", err
	}

	return string(decryptedValue), nil
}
