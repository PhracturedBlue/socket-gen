package main

import (
	"bytes"
	"flag"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"net/netip"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/rjeczalik/notify"
	"github.com/goccy/go-yaml"
	"github.com/google/renameio/v2"
)

/* Steps:
   parse cmdline args (root dir, template-file, output-file, override-dir trigger-cmd)
   add monitor to root-dir, template-file
   scan root-dir
   wait

   scanner:
       find all dirs containing *.sock
         dirname is virtual-host
         path to *.sock is socket
         if exists override.<ext> and override-dir, copy to override-dir/virtualhost.ext
       run templatizer on template-file and write to output-file
       run trigger cmd

    on monitor:
       wait 5 seconds for more changes
       run scanner
*/


type Host struct {
	SocketPath  string
	Name        string
	Overrides   []string
	Config      map[string]string
}

type Func struct {}

type Template struct {
	ListenAddrs []string
	Env         map[string]string
	Hosts       map[string]*Host
	Func        Func
}

type Manual struct {
	Name  string `yaml:name`
	Host  string `yaml:host`
}

var (
	templateFile string
	outputFile   string
	overrideDir  string
	command	     string
	runOnce      bool
	delay        int = 5
	permissions  int = -1
	monitorPaths = []string{"."}
	templateVars Template
)
func init() {
	//usage := "..."
	//flag.CommandLine.Usage = func() {
	//	fmt.Fprintln(os.Stderr, usage)
	//}
	flag.StringVar(&templateFile, "template", "", "template file")
	flag.StringVar(&outputFile, "output", "", "output file")
	flag.StringVar(&overrideDir, "override-dir", "", "directory to place override files in")
	flag.StringVar(&command, "command", "", "command to execute on change")
	flag.IntVar(&delay, "delay", 5, "wait # seconds before updating template and trigger")
	flag.BoolVar(&runOnce, "once", false, "run template once and exit")
	flag.IntVar(&permissions, "permissions", -1, "override socket permissions")

	flag.Parse()
	if flag.NArg() != 0 {
		monitorPaths = flag.Args()
	}
}

func CopyFile(srcpath, dstpath string) (err error) {
	srcStat, err := os.Stat(srcpath)
	if err != nil {
		return err
	}
	dstStat, err := os.Stat(dstpath)
	if err == nil && dstStat.ModTime() == srcStat.ModTime() && dstStat.Size() == srcStat.Size() {
		return nil
	}

        r, err := os.Open(srcpath)
        if err != nil {
                return err
        }
        defer r.Close() // ignore error: file was opened read-only.

        w, err := os.Create(dstpath)
        if err != nil {
                return err
        }

        defer func() {
                // Report the error from Close, if any,
                // but do so only if there isn't already
                // an outgoing error.
                c := w.Close()
		if err == nil {
			if c != nil {
	                        err = c
			} else {
				err = os.Chtimes(dstpath, srcStat.ModTime(), srcStat.ModTime())
			}
		}
        }()

        _, err = io.Copy(w, r)
        return err
}

func ReplaceFile(src string, data []byte) error {
	orig, err := ioutil.ReadFile(src)
	if err == nil && bytes.Equal(orig, data) {
		return nil
	}
        return renameio.WriteFile(src, data, 0o644)
}

// Split splits command line string into command name and command line arguments,
// as expected by the exec.Command function.
// from: https://github.com/rjeczalik/cmd/blob/v1.0.3/internal/cmd/split.go
func SplitCommand(command string) (string, []string) {
	var cmd string
	var args []string
	var i = -1
	var quote rune
	var push = func(n int) {
		if i == -1 {
			return
		}
		if offset := strings.IndexAny(string(command[n-1]), `"'`) ^ -1; cmd == "" {
			cmd = command[i : n+offset]
		} else {
			args = append(args, command[i:n+offset])
		}
	}
	for j, r := range command {
		switch r {
		case '"', '\'', '\\':
			switch quote {
			case 0:
				quote = r
			case '\\', r:
				quote = 0
			}
		case ' ':
			switch quote {
			case 0:
				push(j)
				i = -1
			case '\\':
				quote = 0
			}
		default:
			if i == -1 {
				i = j
			}
		}
	}
	push(len(command))
	return cmd, args
}

func (f Func) MapIndex(item map[string]string, idx string, defval string) string {
	if res, ok := item[idx]; ok {
		return res
	}
	return defval
}

func (f Func) IndexIfExists(items []string, idx int, defval string) string {
	if len(items) < idx {
		return defval
	}
	return items[idx]
}

func (f Func) FileExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		return false
	}
	return true
}

func Scan() {
	log.Println("Scanning...")
	defer log.Println("Scanning complete")
	hosts := make(map[string]*Host)
	paths := []string{}
	for _, monitorpath := range monitorPaths {
		globpaths, err := filepath.Glob(monitorpath + "/*/*")
		if err != nil {
			log.Printf("Could not scan %v: %v\n", monitorpath, err)
			continue
		}
		paths = append(paths, globpaths...)
	}
	for _, filename := range paths {
		s, err := os.Stat(filename)
		if err != nil {
			log.Printf("Could not stat %v\n", filename)
			continue
		}
		hostname := path.Base(path.Dir(filename))
		host, e := hosts[hostname]
		if e == false {
			host = &Host{"", "", []string{}, make(map[string]string)}
			hosts[hostname] = host
		}
		// log.Printf("Found: %v %v %v", filename, path.Base(filename), path.Ext(filename))
		if s.Mode().Type() == fs.ModeSocket {
			host.SocketPath = filename
			if permissions != -1 && (s.Mode().Perm() & os.FileMode(permissions)) != os.FileMode(permissions) {
				os.Chmod(filename, s.Mode().Perm() | os.FileMode(permissions))
			}
				
		} else if path.Base(filename) == "override" + path.Ext(filename) {
			host.Overrides = append(host.Overrides, filename)
			log.Printf("overrides: %v", host.Overrides)
		} else if path.Base(filename) == "host" {
			data, err := ioutil.ReadFile(filename)
			if err != nil {
				log.Printf("Failed to read %v: %v\n", filename, err)
				continue
			}
			content := string(data)
			lines := strings.Split(content, "\n")
			if len(lines) == 1 {
				host.Name = lines[0]
			} else {
				log.Printf("Read %d lines from %v.  Expected 1", len(lines), filename)
			}
		} else if path.Base(filename) == "host.yml" {
			data, err := ioutil.ReadFile(filename)
			if err != nil {
				log.Printf("Failed to read %v: %v\n", filename, err)
				continue
			}
			var m map[string]string
			err = yaml.Unmarshal(data, &m)
			if err != nil {
				log.Printf("Failed to parse %v: %v", filename, err)
				continue
			}
			host.Config = m
			if val, ok := m["name"]; ok {
				host.Name = val
			}
		}

	}

	modhosts := make(map[string]*Host)
	for vhost, obj := range hosts {
		newOverrides := []string {}
		if overrideDir != "" {
			log.Printf("Applying overrides for %v: %v\n", vhost, obj.Overrides)
			for _, override := range obj.Overrides {
				dest := overrideDir + "/" + vhost + path.Ext(override)
				err := CopyFile(override, dest)
				if err != nil {
					log.Printf("Failed to create override file %v: %v\n", dest, err)
				} else {
					newOverrides = append(newOverrides, dest)
				}
			}
		}
		obj.Overrides = newOverrides
		if _, ok := obj.Config["host"]; ! ok {
			obj.Config["host"] = vhost
		}
		modhosts[obj.Config["host"]] = obj
	}
	p, err := ioutil.ReadFile(templateFile)
	if err != nil {
		log.Printf("Failed to read %v: %v\n", templateFile, err)
		return
	}
	tmpl, err := new(template.Template).Parse(string(p))
	if err != nil {
		log.Printf("Failed to parse template for  %v: %v\n", templateFile, err)
		return
	}
	templateVars.Hosts = modhosts
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, templateVars)
	if err != nil {
		log.Printf("Failed to apply template for %v: %v\n", templateFile, err)
		return
	}

	ReplaceFile(outputFile,  buf.Bytes())
        if err != nil {
		log.Printf("Failed to write template to %s: %s\n", outputFile, err)
                return
        }
	if command != "" {
		name, args := SplitCommand(command)
		cmd := exec.Command(name, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			log.Printf("Failed to run tigger: %v\n", err)
		}
	}
}

func ScanMonitor(ch chan bool) {
	for _ = range ch {
		time.Sleep(time.Duration(delay) * time.Second)
		// Clear channel if there were any signals while sleeping
		select {
		case _ = <- ch:
		default:
		}
		Scan()
	}
}

func GetListenAddress() []string {
	SD_LISTEN_FD_START := 3
	if os.Getenv("LISTEN_ADDR") != "" {
		return strings.Split(os.Getenv("LISTEN_ADDR"), " ")
	}
	res := []string {}
	if os.Getenv("LISTEN_FDS") != "" {
		cnt, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
		if err != nil {
			log.Fatalf("Could not parse $LISTEN_FDS")
		}
		for i := SD_LISTEN_FD_START; i < SD_LISTEN_FD_START + cnt; i++ {
			lsa, err := syscall.Getsockname(i)
			if err != nil {
				log.Fatalf("socket-activated file descriptor %d is not a socket: %v", i, err)
			}
			switch lsa.(type) {
			case *syscall.SockaddrUnix:
				res = append(res, lsa.(*syscall.SockaddrUnix).Name)
			case *syscall.SockaddrInet4:
				lsa2 := lsa.(*syscall.SockaddrInet4)
				addr := netip.AddrFrom4(lsa2.Addr)
				res = append(res, addr.String() + ":" + strconv.Itoa(lsa2.Port))
			case *syscall.SockaddrInet6:
				lsa2 := lsa.(*syscall.SockaddrInet6)
				addr := netip.AddrFrom16(lsa2.Addr)
				res = append(res, addr.String() + ":" + strconv.Itoa(lsa2.Port))
			default:
				log.Fatalf("socket-activated file descriptor %d is of unexpected type: %+v", lsa)
			}
		}
		if len(res) == 0 {
			log.Fatalf("No valid socket-activated sockets were found")
		}
	}
	return res
}

func GetEnvVars() map[string]string {
	envpfx := "SOCKETGEN_"
	env := make(map[string]string)
	for _, item := range os.Environ() {
		parsed := strings.SplitN(item, "=", 2)
		if len(parsed) != 2 {
			continue
		}
		if strings.HasPrefix(parsed[0], envpfx) {
			env[parsed[0][len(envpfx):]] = parsed[1]
		}
	}
	return env
}

func main() {
	templateVars = Template{GetListenAddress(), GetEnvVars(), map[string]*Host{}, Func{}}
	if templateFile == "" {
		log.Fatal("-template is a required parameter")
	}
	if outputFile == "" {
		log.Fatal("-output is a required parameter")
	}
	_, err := os.Stat(templateFile)
	if err != nil {
		log.Fatalf("Could not read template file %v: %v", templateFile, err)
	}
	if runOnce {
		Scan()
		return
	}

	// Setup notify
	c := make(chan notify.EventInfo, 1)
	for _, path := range monitorPaths {
		path = path + string(os.PathSeparator) + "..."
		if err := notify.Watch(path, c, notify.All); err != nil {
			log.Fatalf("Failed to create inotify watcher fro %v: %v", path, err)
		}
	}
	if err := notify.Watch(templateFile, c, notify.All); err != nil {
		log.Fatalf("Failed to create inotify watcher for %v: %v", templateFile, err)
	}

	Scan()

	// scanch acts like an event
	scanch := make(chan bool, 1)
	go ScanMonitor(scanch)
	for ei := range c {
		log.Println("received", ei)
		select {
		case scanch <- true: // indicate a change
		default:  // a change is already indicated, no need to duplicate
		}
	}
}
