//go:build !unix

package collator

func stderrWrite(p []byte) (int, error) {
	// 非 Unix 平台兜底，使用 os.Stderr。
	return 0, nil
}