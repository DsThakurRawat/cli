package execx

import (
	"context"
	"testing"
)

func TestNonInteractive_SetsSysProcAttr(t *testing.T) {
	t.Parallel()
	cmd := NonInteractive(context.Background(), "/bin/true")
	if cmd.SysProcAttr == nil {
		t.Skip("SysProcAttr nil — platform without controlling-TTY concept (Windows)")
	}
}

func TestInteractive_NoSysProcAttr(t *testing.T) {
	t.Parallel()
	cmd := Interactive(context.Background(), "/bin/true")
	if cmd.SysProcAttr != nil {
		t.Errorf("Interactive set SysProcAttr = %+v; want nil", cmd.SysProcAttr)
	}
}
