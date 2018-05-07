package service_test

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/obno/service"
)

type testSvc struct{}

func (s *testSvc) Start(svc service.Service) error {
	return nil
}

func (s *testSvc) Stop(svc service.Service) error {
	return nil
}

type upstart struct {
	i service.Interface
	*service.Config
	version string
}

func (s *upstart) hasKillStanza() bool {
	defaultValue := true

	if len(s.version) == 0 {
		//could not get version
		return defaultValue
	}
	return s.versionAtMost("0.6.5")
}

func (s *upstart) hasSetUid() bool {
	defaultValue := true
	if len(s.version) == 0 {
		//could not get version
		return defaultValue
	}
	return s.versionAtLeast("1.4")
}

func (s *upstart) versionAtMost(max string) bool {
	return strings.Compare(s.version, max) <= 0
}

func (s *upstart) versionAtLeast(min string) bool {
	return strings.Compare(s.version, min) >= 0
}

func versionOf(v string) string {
	re := regexp.MustCompile(`init\s+\(upstart\s+([^)]+)\)`)
	if v := re.FindStringSubmatch(v); len(v) == 2 {
		return v[1]
	}
	return ""
}

func (s *upstart) hasStartStopDaemon() bool {
	if _, err := os.Stat("/sbin/start-stop-daemon"); err == nil {
		return true
	}
	return false
}

var tf = map[string]interface{}{
	"cmd": func(s string) string {
		return `"` + strings.Replace(s, `"`, `\"`, -1) + `"`
	},
	"cmdEscape": func(s string) string {
		return strings.Replace(s, " ", `\x20`, -1)
	},
}

func (s *upstart) template() *template.Template {
	return template.Must(template.New("").Funcs(tf).Parse(upstartScript))
}

func TestUpstart(t *testing.T) {

	uss := &upstart{
		i: &testSvc{},
		Config: &service.Config{
			Name:        "test",
			DisplayName: "test",
			Description: "test",
			Executable:  "/some/path/to/exec",
			UserName:    "myrmex",
		},
		version: "",
	}
	uss.version = versionOf("init (upstart 1.4)")
	t.Logf("version: %s", uss.version)

	var to = &struct {
		*service.Config
		Path               string
		HasKillStanza      bool
		HasSetUid          bool
		HasStartStopDaemon bool
	}{
		uss.Config,
		uss.Executable,
		uss.hasKillStanza(),
		uss.hasSetUid(),
		true,
	}

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	uss.template().Execute(writer, to)
	writer.Flush()
	f, err := os.Create("upstart.script")
	if err == nil {
		f.Write(b.Bytes())
		f.Sync()
		f.Close()
	}

}

const upstartScript = `# {{.Description}}

{{if .DisplayName}}description    "{{.DisplayName}}"{{end}}

{{if .HasKillStanza}}kill signal INT{{end}}
{{if .ChRoot}}chroot {{.ChRoot}}{{end}}
{{if .WorkingDirectory}}chdir {{.WorkingDirectory}}{{end}}
start on filesystem or runlevel [2345]
stop on runlevel [!2345]

{{if and .UserName .HasSetUid}}setuid {{.UserName}}{{end}}

respawn
respawn limit 10 5
umask 022

console none

pre-start script
    test -x {{.Path}} || { stop; exit 0; }
end script

# Start
{{if and .UserName (not .HasSetUid)}}
{{if .HasStartStopDaemon}}
exec start-stop-daemon --start -c {{.UserName}} --exec {{.Path}}{{range .Arguments}} {{.|cmd}}{{end}}
{{else}}
exec su -s /bin/sh -c 'exec "$0" "$@"' {{.UserName}} -- {{.Path}}{{range .Arguments}} {{.|cmd}}{{end}}
{{end}}
{{else}}
exec {{.Path}}{{range .Arguments}} {{.|cmd}}{{end}}
{{end}}

`
