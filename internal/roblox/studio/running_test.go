package studio

import "testing"

// The two tasklist outputs below are copied verbatim from Windows, because the
// miss case is what makes this parsing subtle: tasklist exits zero and prints a
// sentence rather than nothing, so an "did the command succeed" check would
// report every machine as running Studio.
func TestProcessListShowsStudio(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		goos string
		want bool
	}{
		{
			name: "windows lists the process",
			out:  "RobloxStudioBeta.exe          4996 Console                    1    459 976 K\r\n",
			goos: "windows",
			want: true,
		},
		{
			name: "windows filter matched nothing",
			out:  "INFO: No tasks are running which match the specified criteria.\r\n",
			goos: "windows",
			want: false,
		},
		{
			name: "windows lists another process only",
			out:  "explorer.exe                  8384 Console                    1    459 976 K\r\n",
			goos: "windows",
			want: false,
		},
		{
			name: "pgrep prints a pid",
			out:  "4996\n",
			goos: "darwin",
			want: true,
		},
		{
			name: "pgrep prints nothing",
			out:  "\n",
			goos: "darwin",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := processListShowsStudio(tc.out, tc.goos); got != tc.want {
				t.Errorf("processListShowsStudio(%q, %s) = %v, want %v", tc.out, tc.goos, got, tc.want)
			}
		})
	}
}
