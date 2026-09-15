package main

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

const timeSecond = time.Second

// ctxWithCancel 把 channel 转成 context.Context。
//
// 当 cancelCh 被关闭（或收到任意值）时，ctx.Done() 会被触发。
// collator 当前未消费 ctx，但预留接口以便将来支持优雅中断。
func ctxWithCancel(cancelCh <-chan struct{}) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-cancelCh
		cancel()
	}()
	return ctx
}

// openInOS 用系统文件管理器（Finder / Explorer / xdg-open）打开目录。
func openInOS(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// timeAfterFunc 是 time.AfterFunc 的本地别名，方便 GUI 调用。
func timeAfterFunc(d time.Duration, f func()) *time.Timer {
	return time.AfterFunc(d, f)
}