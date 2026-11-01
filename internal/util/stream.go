package util

import (
	"context"
	"errors"
	"io"
)

// 本文件提供 IO 流层面的通用操作。

// ErrSizeLimit 表示复制的内容超过了允许的大小上限。
var ErrSizeLimit = errors.New("解出内容超过大小上限")

// CopyWithContext 把 src 复制到 dst，并在超过 limit 字节时中止。
//
// ctx 为 nil 时不做取消检查。limit <= 0 表示不限制大小。
// 返回实际写出的字节数与遇到的错误。
func CopyWithContext(ctx context.Context, dst io.Writer, src io.Reader, limit int64) (int64, error) {
	buf := make([]byte, 256*1024)
	var total int64
	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return total, ctx.Err()
			default:
			}
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			if limit > 0 && total+int64(n) > limit {
				// 超出限制时只写入允许的部分。
				allowed := limit - total
				if allowed > 0 {
					if _, err := dst.Write(buf[:allowed]); err != nil {
						return total, err
					}
					total += allowed
				}
				return total, ErrSizeLimit
			}
			written, writeErr := dst.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}
