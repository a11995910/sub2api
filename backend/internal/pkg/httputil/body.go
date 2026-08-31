package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	requestBodyReadInitCap    = 512
	requestBodyReadMaxInitCap = 1 << 20
	jsonUTF8BOMLen            = 3
	// MaxDecompressedBodySize 限制解压后的请求体大小，防止解压炸弹。
	MaxDecompressedBodySize int64 = 64 << 20
	// 保留包内别名，避免已有包内测试和调用点改变。
	maxDecompressedBodySize = MaxDecompressedBodySize
)

// ReadRequestBodyWithPrealloc reads request body with preallocated buffer based
// on content length, transparently decoding any Content-Encoding the upstream
// client used to compress the body (zstd, gzip, deflate).
func ReadRequestBodyWithPrealloc(req *http.Request) ([]byte, error) {
	if cached, ok := CachedRequestBody(req); ok {
		// 缓存体可能已经被前一个消费者读完，命中时恢复 reader，保证后续
		// handler/转发逻辑仍可重复读取同一份请求体。
		ResetRequestBody(req, cached)
		return cached, nil
	}
	if req == nil || req.Body == nil {
		return nil, nil
	}

	capHint := requestBodyReadInitCap
	if req.ContentLength > 0 {
		switch {
		case req.ContentLength < int64(requestBodyReadInitCap):
			capHint = requestBodyReadInitCap
		case req.ContentLength > int64(requestBodyReadMaxInitCap):
			capHint = requestBodyReadMaxInitCap
		default:
			capHint = int(req.ContentLength)
		}
	}

	buf := bytes.NewBuffer(make([]byte, 0, capHint))
	if _, err := io.Copy(buf, req.Body); err != nil {
		return nil, err
	}
	raw := buf.Bytes()

	enc := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
	if enc == "" || enc == "identity" {
		return raw, nil
	}

	decoded, err := decompressRequestBodyWithLimit(enc, raw, requestDecompressedBodyLimit(req))
	if err != nil {
		return nil, fmt.Errorf("decode Content-Encoding %q: %w", enc, err)
	}

	req.Header.Del("Content-Encoding")
	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(decoded))

	return decoded, nil
}

// ReadLenientJSONRequestBodyWithPrealloc reads a request body and normalizes
// JSON string control bytes before strict validation.
func ReadLenientJSONRequestBodyWithPrealloc(req *http.Request, maxNormalizedBytes int64) ([]byte, error) {
	body, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		return nil, err
	}
	// 网关准入会把实际路由（例如纯文本端点）的限制写入 request
	// context。规范化可能把一个控制字节扩展成六字节，不能因为调用方
	// 传入了全局上限而绕过更小的请求级限制。
	if requestLimit := DecompressedBodyLimit(req); requestLimit > 0 &&
		(maxNormalizedBytes <= 0 || requestLimit < maxNormalizedBytes) {
		maxNormalizedBytes = requestLimit
	}
	normalized, err := NormalizeLenientJSONRequestBody(body, maxNormalizedBytes)
	if err != nil {
		return nil, err
	}
	// 复合路由会把 body 放进请求缓存；规范化后同步替换缓存，避免
	// handler 再次命中旧的、尚未转义的内容。
	if _, cached := CachedRequestBody(req); cached && !bytes.Equal(body, normalized) {
		WithCachedRequestBody(req, normalized)
	}
	return normalized, nil
}

func decompressRequestBody(encoding string, raw []byte) ([]byte, error) {
	return decompressRequestBodyWithLimit(encoding, raw, MaxDecompressedBodySize)
}

func decompressRequestBodyWithLimit(encoding string, raw []byte, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > MaxDecompressedBodySize {
		maxBytes = MaxDecompressedBodySize
	}
	switch encoding {
	case "zstd":
		// zstd 默认允许较大的窗口和多路解码器。请求体已由网关准入层
		// 限制，这里继续把解码器限制到单并发和同等内存上限，避免恶意
		// frame 在输出截断前先申请超出预算的窗口。
		decoderMaxMemory := maxBytes
		if decoderMaxMemory < zstd.MinWindowSize {
			decoderMaxMemory = zstd.MinWindowSize
		}
		dec, err := zstd.NewReader(
			bytes.NewReader(raw),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(uint64(decoderMaxMemory)),
		)
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		return readDecompressedBodyWithLimit(dec, maxBytes)
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		return readDecompressedBodyWithLimit(gr, maxBytes)
	case "deflate":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer func() { _ = zr.Close() }()
		return readDecompressedBodyWithLimit(zr, maxBytes)
	default:
		return nil, errors.New("unsupported Content-Encoding")
	}
}

func readDecompressedBody(reader io.Reader) ([]byte, error) {
	return readDecompressedBodyWithLimit(reader, MaxDecompressedBodySize)
}

func readDecompressedBodyWithLimit(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > MaxDecompressedBodySize {
		maxBytes = MaxDecompressedBodySize
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(decoded)) > maxBytes {
		return nil, &http.MaxBytesError{Limit: maxBytes}
	}
	return decoded, nil
}

func requestDecompressedBodyLimit(req *http.Request) int64 {
	if limit := DecompressedBodyLimit(req); limit > 0 && limit < MaxDecompressedBodySize {
		return limit
	}
	return MaxDecompressedBodySize
}

// NormalizeLenientJSONRequestBody escapes raw control bytes that broken
// OpenAI-compatible clients sometimes place inside JSON strings.
func NormalizeLenientJSONRequestBody(body []byte, maxNormalizedBytes int64) ([]byte, error) {
	if maxNormalizedBytes <= 0 {
		maxNormalizedBytes = maxDecompressedBodySize
	}

	body = trimUTF8BOM(body)
	if len(body) == 0 {
		return body, nil
	}
	if int64(len(body)) > maxNormalizedBytes {
		return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
	}

	var out []byte
	inString := false
	escaped := false
	for i, b := range body {
		if inString && isJSONControlByte(b) {
			if out == nil {
				capHint := len(body) + 6
				if int64(capHint) > maxNormalizedBytes {
					capHint = int(maxNormalizedBytes)
				}
				out = make([]byte, 0, capHint)
				out = append(out, body[:i]...)
			}
			if int64(len(out)+6) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = appendJSONUnicodeEscape(out, b)
			escaped = false
			continue
		}

		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		}

		if out != nil {
			if int64(len(out)+1) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = append(out, b)
		}
	}
	if out != nil {
		return out, nil
	}
	return body, nil
}

func trimUTF8BOM(body []byte) []byte {
	if len(body) >= jsonUTF8BOMLen && body[0] == 0xef && body[1] == 0xbb && body[2] == 0xbf {
		return body[jsonUTF8BOMLen:]
	}
	return body
}

func isJSONControlByte(b byte) bool {
	return b < 0x20 || b == 0x7f
}

func appendJSONUnicodeEscape(dst []byte, b byte) []byte {
	const hex = "0123456789abcdef"
	return append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
}
