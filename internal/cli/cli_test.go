package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		wantExit      int
		wantStdoutHas string
		wantStderrHas string
	}{
		{
			name:          "no args prints usage",
			args:          nil,
			wantExit:      exitOK,
			wantStdoutHas: "Usage:",
		},
		{
			name:          "help flag",
			args:          []string{"--help"},
			wantExit:      exitOK,
			wantStdoutHas: "Usage:",
		},
		{
			name:          "version subcommand",
			args:          []string{"version"},
			wantExit:      exitOK,
			wantStdoutHas: "ceroc " + Version,
		},
		{
			name:          "version flag",
			args:          []string{"-v"},
			wantExit:      exitOK,
			wantStdoutHas: "ceroc " + Version,
		},
		{
			name:          "build is not yet implemented",
			args:          []string{"build", "main.cero"},
			wantExit:      exitNotImplemented,
			wantStderrHas: "not yet implemented",
		},
		{
			name:          "run is not yet implemented",
			args:          []string{"run", "main.cero"},
			wantExit:      exitNotImplemented,
			wantStderrHas: "not yet implemented",
		},
		{
			name:          "fmt is not yet implemented",
			args:          []string{"fmt"},
			wantExit:      exitNotImplemented,
			wantStderrHas: "not yet implemented",
		},
		{
			name:          "unknown command is a usage error",
			args:          []string{"frobnicate"},
			wantExit:      exitUsageError,
			wantStderrHas: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			got := Run(tt.args, &stdout, &stderr)

			if got != tt.wantExit {
				t.Errorf("Run(%v) exit code = %d, want %d", tt.args, got, tt.wantExit)
			}
			if tt.wantStdoutHas != "" && !strings.Contains(stdout.String(), tt.wantStdoutHas) {
				t.Errorf("Run(%v) stdout = %q, want to contain %q", tt.args, stdout.String(), tt.wantStdoutHas)
			}
			if tt.wantStderrHas != "" && !strings.Contains(stderr.String(), tt.wantStderrHas) {
				t.Errorf("Run(%v) stderr = %q, want to contain %q", tt.args, stderr.String(), tt.wantStderrHas)
			}
		})
	}
}
