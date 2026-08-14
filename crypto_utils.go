package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PKCS7Padding 填充
func PKCS7Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

// AESEncrypt CBC模式加密
func AESEncrypt(text string, keyHex, ivHex string) (string, error) {
	key, _ := hex.DecodeString(keyHex)
	iv, _ := hex.DecodeString(ivHex)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	plaintext := PKCS7Padding([]byte(text), block.BlockSize())
	blockMode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(plaintext))
	blockMode.CryptBlocks(ciphertext, plaintext)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// RSAEncrypt PKCS1v15加密
func RSAEncrypt(text string, publicKeyB64 string) (string, error) {
	keyData, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "", err
	}

	// 尝试作为 DER 解析
	pub, err := x509.ParsePKIXPublicKey(keyData)
	if err != nil {
		// 尝试作为 PEM 解析
		block, _ := pem.Decode([]byte(fmt.Sprintf("-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----", publicKeyB64)))
		if block == nil {
			return "", errors.New("failed to parse public key")
		}
		pub, err = x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return "", err
		}
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("not an RSA public key")
	}

	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(text))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// GenerateP0K0Dynamic 生成 p0 和 k0
func GenerateP0K0Dynamic(vtoken string, p0Params map[string]string, rsaPublicKey string) (string, string, error) {
	// 生成随机 AES Key (32字节) 和 IV (16字节)
	aesKey := make([]byte, 32)
	aesIV := make([]byte, 16)
	rand.Read(aesKey)
	rand.Read(aesIV)

	aesKeyHex := hex.EncodeToString(aesKey)
	aesIVHex := hex.EncodeToString(aesIV)

	// vtoken 转 base64
	vtokenB64 := base64.StdEncoding.EncodeToString([]byte(vtoken))

	b64 := func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	}

	wm := p0Params["wm"]
	if wm == "" {
		wm = "7890747"
	}

	p0Plain := fmt.Sprintf(
		"app_v--%s||brand--%s||p_type--%s||sdk_v--%s||sys--%s||sys_ver--%s||vtoken--%s||wm--%s",
		b64(p0Params["app_v"]),
		b64(p0Params["brand"]),
		b64(p0Params["p_type"]),
		b64(p0Params["sdk_v"]),
		b64(p0Params["sys"]),
		b64(p0Params["sys_ver"]),
		vtokenB64,
		b64(wm),
	)

	// AES 加密 p0
	p0, err := AESEncrypt(p0Plain, aesKeyHex, aesIVHex)
	if err != nil {
		return "", "", err
	}

	// RSA 加密 AES Key + IV
	rsaPlain := base64.StdEncoding.EncodeToString(aesKey) + "\n" + base64.StdEncoding.EncodeToString(aesIV)
	k0, err := RSAEncrypt(rsaPlain, rsaPublicKey)
	if err != nil {
		return "", "", err
	}

	return p0, k0, nil
}

// GetGACode 实现 TOTP 算法 (GA 验证码)

// GetGACode 实现 TOTP 算法 (GA 谷歌身份验证器)
// 对应易语言 谷歌身份验证生成 子程序 by 7ian 大圣 541980200
func GetGACode(secret string) string {
	// 1. Base32 解码密钥 (转大写后解码)
	secret = strings.ToUpper(strings.TrimSpace(secret))
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		return "000000"
	}

	// 2. 计算时间步长 (t / 30)
	// 对应易语言: t ＝ 取时间间隔 (取现行时间 (), [1970年1月1日8时], #秒)
	counter := time.Now().Unix() / 30

	// 3. 将计数器转为 8 字节大端字节集
	// 对应易语言: 字节集翻转 (到字节集 (到长整数 (t ÷ 30)))
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	// 4. HMAC-SHA1 计算
	// 对应易语言: h ＝ sha1 (字节集翻转 (...), base32解码 (到大写 (密钥)))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	hash := mac.Sum(nil)

	// 5. 动态截断 (Dynamic Truncation)
	// 对应易语言: 位与 (h [l], 15) 取偏移量
	offset := hash[len(hash)-1] & 0x0F

	// 对应易语言: 取字节集数据 (字节集翻转 (h), #整数型, l - 位与 (h [l], 15) - 3)
	// 从 offset 处取 4 字节作为大端整数
	binaryNum := binary.BigEndian.Uint32(hash[offset : offset+4])

	// 对应易语言: 位与 (..., 2147483647)
	// 去掉最高位（符号位），2147483647 = 0x7FFFFFFF
	code := binaryNum & 0x7FFFFFFF

	// 6. 取最后 6 位，不足补零
	// 对应易语言: 取文本右边 ("000000000" + ..., 6)
	return fmt.Sprintf("%06d", code%1000000)
}
