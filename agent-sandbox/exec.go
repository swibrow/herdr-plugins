package main

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

//go:embed session.sh
var sessionScript string

func (k *kube) execSession(ctx context.Context, pod string, src source) (int, error) {
	req := k.core.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(k.cfg.Namespace).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "sandbox",
			Command:   []string{"bash", "-c", sessionScript, "session.sh", src.Repo, src.Ref},
			Stdin:     true,
			Stdout:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	ws, err := remotecommand.NewWebSocketExecutor(k.rest, "GET", req.URL().String())
	if err != nil {
		return 0, err
	}
	spdy, err := remotecommand.NewSPDYExecutor(k.rest, "POST", req.URL())
	if err != nil {
		return 0, err
	}
	executor, err := remotecommand.NewFallbackExecutor(ws, spdy, func(err error) bool {
		return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
	})
	if err != nil {
		return 0, err
	}

	fd := int(os.Stdin.Fd())
	if old, err := term.MakeRaw(fd); err == nil {
		defer term.Restore(fd, old)
	}

	sizes := newSizeQueue(fd)
	defer sizes.stop()

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             os.Stdin,
		Stdout:            os.Stdout,
		Tty:               true,
		TerminalSizeQueue: sizes,
	})
	var exitErr utilexec.CodeExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code, nil
	}
	return 0, err
}

type sizeQueue struct {
	ch   chan *remotecommand.TerminalSize
	sig  chan os.Signal
	done chan struct{}
}

func newSizeQueue(fd int) *sizeQueue {
	q := &sizeQueue{
		ch:   make(chan *remotecommand.TerminalSize, 1),
		sig:  make(chan os.Signal, 1),
		done: make(chan struct{}),
	}
	signal.Notify(q.sig, syscall.SIGWINCH)
	send := func() {
		if w, h, err := term.GetSize(fd); err == nil {
			select {
			case q.ch <- &remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}:
			default:
			}
		}
	}
	send()
	go func() {
		for {
			select {
			case <-q.sig:
				send()
			case <-q.done:
				return
			}
		}
	}()
	return q
}

func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case s := <-q.ch:
		return s
	case <-q.done:
		return nil
	}
}

func (q *sizeQueue) stop() {
	signal.Stop(q.sig)
	close(q.done)
}
