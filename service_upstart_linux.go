// Copyright 2015 Daniel Theophanes.
// Use of this source code is governed by a zlib-style
// license that can be found in the LICENSE file.

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"text/template"
	"time"
)

func isUpstart() bool {
	if _, err := os.Stat("/sbin/upstart-udev-bridge"); err == nil {
		return true
	}
	if _, err := os.Stat("/sbin/init"); err == nil {
		if out, err := exec.Command("/sbin/init", "--version").Output(); err == nil {
			if strings.Contains(string(out), "init (upstart") {
				return true
			}
		}
	}
	return false
}

type upstart struct {
	i Interface
	*Config
	version string
}

func newUpstartService(i Interface, c *Config) (Service, error) {
	s := &upstart{
		i:       i,
		Config:  c,
		version: upstartVersion(),
	}

	return s, nil
}

func (s *upstart) String() string {
	if len(s.DisplayName) > 0 {
		return s.DisplayName
	}
	return s.Name
}

// Upstart has some support for user services in graphical sessions.
// Due to the mix of actual support for user services over versions, just don't bother.
// Upstart will be replaced by systemd in most cases anyway.
var errNoUserServiceUpstart = errors.New("User services are not supported on Upstart.")

func (s *upstart) configPath() (cp string, err error) {
	if s.Option.bool(optionUserService, optionUserServiceDefault) {
		err = errNoUserServiceUpstart
		return
	}
	cp = "/etc/init/" + s.Config.Name + ".conf"
	return
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

func (s *upstart) hasStartStopDaemon() bool {
	if _, err := os.Stat("/sbin/start-stop-daemon"); err == nil {
		return true
	}
	return false
}

func upstartVersion() string {
	out, err := exec.Command("/sbin/init", "--version").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`init\s+\(upstart\s+([^)]+)\)`)
	if v := re.FindStringSubmatch(string(out)); len(v) == 2 {
		return v[1]
	}
	return ""
}

func (s *upstart) versionAtMost(max string) bool {
	return strings.Compare(s.version, max) <= 0
}

func (s *upstart) versionAtLeast(min string) bool {
	return strings.Compare(s.version, min) >= 0
}

func (s *upstart) template() *template.Template {
	return template.Must(template.New("").Funcs(tf).Parse(upstartScript))
}

func (s *upstart) Install() error {
	confPath, err := s.configPath()
	if err != nil {
		return err
	}
	_, err = os.Stat(confPath)
	if err == nil {
		return fmt.Errorf("Init already exists: %s", confPath)
	}

	f, err := os.Create(confPath)
	if err != nil {
		return err
	}
	defer f.Close()

	path, err := s.execPath()
	if err != nil {
		return err
	}

	var to = &struct {
		*Config
		Path               string
		HasKillStanza      bool
		HasSetUid          bool
		HasStartStopDaemon bool
	}{
		s.Config,
		path,
		s.hasKillStanza(),
		s.hasSetUid(),
		s.hasStartStopDaemon(),
	}

	return s.template().Execute(f, to)
}

func (s *upstart) Uninstall() error {
	cp, err := s.configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(cp); err != nil {
		return err
	}
	return nil
}

func (s *upstart) Logger(errs chan<- error) (Logger, error) {
	if system.Interactive() {
		return ConsoleLogger, nil
	}
	return s.SystemLogger(errs)
}
func (s *upstart) SystemLogger(errs chan<- error) (Logger, error) {
	return newSysLogger(s.Name, errs)
}

func (s *upstart) Run() (err error) {
	err = s.i.Start(s)
	if err != nil {
		return err
	}

	s.Option.funcSingle(optionRunWait, func() {
		var sigChan = make(chan os.Signal, 3)
		signal.Notify(sigChan, os.Interrupt, os.Kill)
		<-sigChan
	})()

	return s.i.Stop(s)
}

func (s *upstart) Start() error {
	return run("initctl", "start", s.Name)
}

func (s *upstart) Stop() error {
	return run("initctl", "stop", s.Name)
}

func (s *upstart) Restart() error {
	err := s.Stop()
	if err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return s.Start()
}

// The upstart script should stop with an INT or the Go runtime will terminate
// the program before the Stop handler can run.
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
