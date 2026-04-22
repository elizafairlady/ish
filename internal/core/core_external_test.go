package core_test

import (
	"testing"

	"ish/internal/core"
	"ish/internal/process"
)

func TestStringPid(t *testing.T) {
	p := process.NewProcess()
	defer p.Close()
	v := core.PidVal(p)
	got := v.String()
	if got == "#PID<nil>" {
		t.Errorf("expected a valid PID string, got %q", got)
	}
}

func TestEqualPid(t *testing.T) {
	p1 := process.NewProcess()
	p2 := process.NewProcess()
	defer p1.Close()
	defer p2.Close()

	v1 := core.PidVal(p1)
	v2 := core.PidVal(p1)
	v3 := core.PidVal(p2)

	if !v1.Equal(v2) {
		t.Error("same process should be equal")
	}
	if v1.Equal(v3) {
		t.Error("different processes should not be equal")
	}
	vNil := core.PidVal(nil)
	if v1.Equal(vNil) {
		t.Error("pid should not equal nil pid")
	}
	if !vNil.Equal(vNil) {
		t.Error("nil pid should equal nil pid")
	}
}

func TestEnvGetProc(t *testing.T) {
	t.Run("direct proc on ctx", func(t *testing.T) {
		e := core.NewEnv(nil)
		p := process.NewProcess()
		defer p.Close()
		e.Ctx.Proc = p

		if e.Ctx.Proc != p {
			t.Error("expected Proc on Ctx")
		}
	})

	t.Run("child shares parent ctx proc", func(t *testing.T) {
		parent := core.NewEnv(nil)
		p := process.NewProcess()
		defer p.Close()
		parent.Ctx.Proc = p

		child := core.NewEnv(parent)
		// Child shares parent's ExecCtx, so Proc is visible
		if child.Ctx.Proc != p {
			t.Error("expected child to see parent's proc via shared Ctx")
		}
	})

	t.Run("nil when no proc", func(t *testing.T) {
		e := core.NewEnv(nil)
		if e.Ctx.Proc != nil {
			t.Error("expected nil when no proc set")
		}
	})
}
