// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"sync"

	"vanilla-router/internal/stickwrite"
	"vanilla-router/internal/windisk"
)

// Flow: manifest → (download) → verify → open stick → write → check (PLAN.md 3.12).
type phase int

const (
	phIdle phase = iota
	phManifest
	phDownload
	phCheck
	phOpen
	phWrite
	phVerify
)

var errOnStick = errors.New("image is on the target")

// device is an opened stick.
type device interface {
	stickwrite.Device
	Close() error
}

// platform wraps what only works on Windows, so the flow can be tested on Linux.
type platform struct {
	open        func(windisk.Disk) (device, error)
	disksOfPath func(string) []int
}

// job is a running operation. The window polls its state with a timer.
type job struct {
	mu      sync.Mutex
	ph      phase
	done    int64
	total   int64
	wrote   bool // the stick has already been changed
	err     error
	over    bool
	cancel  context.CancelFunc
	im      *image
	offline bool
}

type jobState struct {
	ph          phase
	done, total int64
	wrote, over bool
	err         error
	im          *image
	offline     bool
}

func (j *job) set(ph phase, done, total int64) {
	j.mu.Lock()
	j.ph, j.done, j.total = ph, done, total
	if ph >= phWrite {
		j.wrote = true
	}
	j.mu.Unlock()
}

func (j *job) state() jobState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return jobState{j.ph, j.done, j.total, j.wrote, j.over, j.err, j.im, j.offline}
}

func (j *job) finish(err error) {
	j.mu.Lock()
	j.err, j.over = err, true
	j.mu.Unlock()
}

// prepare fetches the manifest (or checks the chosen file), so that it is known before the warning
// whether the stick is big enough.
func prepare(ctx context.Context, j *job, src *source, local *image) {
	if local != nil {
		j.mu.Lock()
		j.im = local
		j.mu.Unlock()
		j.finish(nil)
		return
	}
	j.set(phManifest, 0, 0)
	m, offline, err := src.manifest(ctx)
	if err == nil {
		j.mu.Lock()
		j.im, j.offline = &image{m: m}, offline
		j.mu.Unlock()
	}
	j.finish(err)
}

// run writes the stick. stickJSON holds the defaults from Windows (F-STICK-07).
func run(ctx context.Context, j *job, src *source, im *image, d windisk.Disk, p platform, stickJSON []byte) {
	err := runSteps(ctx, j, src, im, d, p, stickJSON)
	discardCorrupt(j.state().im, err)
	j.finish(err)
}

func runSteps(ctx context.Context, j *job, src *source, im *image, d windisk.Disk, p platform, stickJSON []byte) error {
	if d.Size < im.m.Image.Size {
		return stickwrite.ErrTooSmall
	}
	if im.path == "" {
		j.set(phDownload, 0, im.m.File.Size)
		got, err := src.download(ctx, im.m, func(done, total int64) { j.set(phDownload, done, total) })
		if err != nil {
			return err
		}
		im = got
	}
	j.mu.Lock()
	j.im = im
	j.mu.Unlock()
	if p.disksOfPath != nil {
		for _, n := range p.disksOfPath(im.path) {
			if n == d.Number {
				return errOnStick
			}
		}
	}
	prog := func(ph stickwrite.Phase, done, total int64) {
		switch ph {
		case stickwrite.PhaseCheck:
			j.set(phCheck, done, total)
		case stickwrite.PhaseWrite:
			j.set(phWrite, done, total)
		case stickwrite.PhaseVerify:
			j.set(phVerify, done, total)
		}
	}
	j.set(phCheck, 0, im.m.Image.Size)
	if err := stickwrite.Check(ctx, im.open, im.m, prog); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	j.set(phOpen, 0, 0)
	dev, err := p.open(d)
	if err != nil {
		return err
	}
	j.set(phWrite, 0, im.m.Image.Size)
	werr := stickwrite.Write(ctx, im.open, im.m, dev, stickJSON, prog)
	cerr := dev.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
