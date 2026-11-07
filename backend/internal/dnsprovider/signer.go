package dnsprovider

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ===== helpers =====

func sha256hex(s string) string {
	b := sha256.Sum256([]byte(s))
	return hex.EncodeToString(b[:])
}

func hmacSHA256(key, data string) []byte {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(data))
	return m.Sum(nil)
}

// ===== Tencent Cloud (TC3-HMAC-SHA256) =====
// https://cloud.tencent.com/document/api/1427/56189

func signTencent(req *http.Request, secretID, secretKey, service, action, version, payload string) {
	const algorithm = "TC3-HMAC-SHA256"
	host := service + ".tencentcloudapi.com"

	ts := time.Now().Unix()
	tsStr := strconv.FormatInt(ts, 10)

	canonicalHeaders := "content-type:application/json\nhost:" + host + "\nx-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256hex(payload)

	date := time.Unix(ts, 0).UTC().Format("2006-01-02")
	scope := date + "/" + service + "/tc3_request"
	stringToSign := algorithm + "\n" + tsStr + "\n" + scope + "\n" + sha256hex(canonicalRequest)

	secretDate := hmacSHA256("TC3"+secretKey, date)
	secretService := hmacSHA256(string(secretDate), service)
	secretSigning := hmacSHA256(string(secretService), "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(string(secretSigning), stringToSign))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", algorithm+" Credential="+secretID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Timestamp", tsStr)
	req.Header.Set("X-TC-Version", version)
	req.Host = host
}

// ===== Aliyun RPC signature (HMAC-SHA1) =====
// https://help.aliyun.com/document_detail/29776.html

func aliyunSign(accessKeyID, accessSecret string, params url.Values, method, version string) {
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureNonce", strconv.FormatInt(time.Now().UnixNano(), 10))
	params.Set("AccessKeyId", accessKeyID)
	params.Set("SignatureVersion", "1.0")
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("Format", "JSON")
	params.Set("Version", version)
	params.Set("Signature", aliyunHmacB64(method, accessSecret, params))
}

func aliyunHmacB64(method, secret string, vals url.Values) string {
	var buf bytes.Buffer
	buf.WriteString(method)
	buf.WriteString("&")
	buf.WriteString(aliyunEncode("/"))
	buf.WriteString("&")
	buf.WriteString(aliyunEncode(vals.Encode()))

	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write(buf.Bytes())
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// aliyunEncode follows Aliyun's special URL encoding rules.
func aliyunEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '%':
			if i+3 <= len(s) && s[i:i+3] == "%7E" {
				b.WriteByte('~')
				i += 2
				continue
			}
			b.WriteString("%25")
		case '*', '/', '&', '=':
			fmt.Fprintf(&b, "%%%02X", ch)
		case '+':
			b.WriteString("%20")
		case '~':
			b.WriteByte('~')
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// ===== Huawei Cloud (SDK-HMAC-SHA256) =====

const huaweiDateFormat = "20060102T150405Z"

func signHuawei(req *http.Request, ak, sk string) error {
	if req.Header.Get("X-Sdk-Date") == "" {
		req.Header.Set("X-Sdk-Date", time.Now().UTC().Format(huaweiDateFormat))
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))

	signedHeaders := []string{"content-type", "host", "x-sdk-date"}
	headerBlock := "content-type:" + strings.TrimSpace(req.Header.Get("content-type")) + "\n" +
		"host:" + req.Host + "\n" +
		"x-sdk-date:" + req.Header.Get("X-Sdk-Date") + "\n"

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		canonicalQuery(req.URL),
		headerBlock,
		strings.Join(signedHeaders, ";"),
		sha256hex(string(body)),
	}, "\n")

	stringToSign := "SDK-HMAC-SHA256\n" + req.Header.Get("X-Sdk-Date") + "\n" + sha256hex(canonicalRequest)
	signature := hex.EncodeToString(hmacSHA256(sk, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf("SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s",
		ak, strings.Join(signedHeaders, ";"), signature))
	return nil
}

func canonicalURI(p string) string {
	parts := strings.Split(p, "/")
	for i, v := range parts {
		parts[i] = url.PathEscape(v)
	}
	out := strings.Join(parts, "/")
	if !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
}

func canonicalQuery(u *url.URL) string {
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, url.PathEscape(k)+"="+url.PathEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

// ===== Baidu Cloud BCE =====
// https://cloud.baidu.com/doc/Reference/s/Njwvz1wot

func signBaidu(req *http.Request, ak, sk, host string) error {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))

	prefix := "bce-auth-v1/" + ak + "/" + time.Now().UTC().Format("2006-01-02T15:04:05Z") + "/1800"
	canonicalReq := req.Method + "\n" + baiduCanonicalURI(req.URL.Path) + "\n" + req.URL.RawQuery + "\n" + "host:" + host

	signingKey := baiduHmac(sk, prefix)
	signature := baiduHmac(signingKey, canonicalReq)
	req.Header.Set("Authorization", prefix+"/host/"+signature)
	return nil
}

func baiduHmac(secret, message string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(message))
	return hex.EncodeToString(m.Sum(nil))
}

func baiduCanonicalURI(p string) string {
	parts := strings.Split(p, "/")
	for i, v := range parts {
		parts[i] = url.PathEscape(v)
	}
	out := strings.Join(parts, "/")
	return strings.TrimSuffix(out, "/")
}
