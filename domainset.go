package main

import (
	"bufio"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
)

type domainCmd int

// Basically a concurrent map. I don't want to use channels to implement
// concurrent access to this as I'm comfortable to use locks for simple tasks
// like this
type domainSet struct {
	sync.RWMutex
	domain map[string]bool
}

func newDomainSet() *domainSet {
	ds := new(domainSet)
	ds.domain = make(map[string]bool)
	return ds
}

func (ds *domainSet) add(dm string) {
	ds.Lock()
	ds.domain[dm] = true
	ds.Unlock()
}

func (ds *domainSet) has(dm string) bool {
	ds.RLock()
	_, ok := ds.domain[dm]
	ds.RUnlock()
	return ok
}

func (ds *domainSet) del(dm string) {
	ds.Lock()
	delete(ds.domain, dm)
	ds.Unlock()
}

func (ds *domainSet) loadDomainList(fpath string) (lst []string, err error) {
	lst, err = loadDomainList(fpath)
	if err != nil {
		return
	}
	// This executes in single goroutine, so no need to use lock
	for _, v := range lst {
		// debug.Println("loaded domain:", v)
		ds.domain[v] = true
	}
	return
}

func (ds *domainSet) toArray() []string {
	l := len(ds.domain)
	lst := make([]string, l, l)

	i := 0
	for k, _ := range ds.domain {
		lst[i] = k
		i++
	}
	return lst
}

var blockedDs = newDomainSet()
var directDs = newDomainSet()

var blockedDomainChanged = false
var directDomainChanged = false

func isRequestBlocked(r *Request) bool {
	host, _ := splitHostPort(r.URL.Host)
	return blockedDs.has(host2Domain(host))
}

func addBlockedRequest(r *Request) {
	host, _ := splitHostPort(r.URL.Host)
	if hostIsIP(host) {
		return
	}
	dm := host2Domain(host)
	if !blockedDs.has(dm) {
		blockedDs.add(dm)
		blockedDomainChanged = true
		debug.Printf("%v added to blocked list\n", dm)
	}
	// Delete this request from direct domain set
	delDirectRequest(r)
}

func delBlockedRequest(r *Request) {
	host, _ := splitHostPort(r.URL.Host)
	dm := host2Domain(host)
	if blockedDs.has(dm) {
		blockedDs.del(dm)
		blockedDomainChanged = true
		debug.Printf("%v deleted from blocked list\n", dm)
	}
}

func addDirectRequest(r *Request) {
	host, _ := splitHostPort(r.URL.Host)
	if hostIsIP(host) {
		return
	}
	dm := host2Domain(host)
	if !directDs.has(dm) {
		directDs.add(dm)
		directDomainChanged = true
	}
	// Delete this request from blocked domain set
	delBlockedRequest(r)
}

func delDirectRequest(r *Request) {
	host, _ := splitHostPort(r.URL.Host)
	dm := host2Domain(host)
	if directDs.has(dm) {
		directDs.del(dm)
		directDomainChanged = true
	}
}

func writeBlockedDs() {
	if !blockedDomainChanged {
		return
	}
	writeDomainList(config.blockedFile, blockedDs.toArray())
}

func writeDirectDs() {
	if !directDomainChanged {
		return
	}
	writeDomainList(config.directFile, directDs.toArray())
}

func loadDomainList(fpath string) (lst []string, err error) {
	f, err := openFile(fpath)
	if f == nil || err != nil {
		return
	}
	defer f.Close()

	fr := bufio.NewReader(f)
	lst = make([]string, 0)
	var domain string
	for {
		domain, err = ReadLine(fr)
		if err == io.EOF {
			return lst, nil
		} else if err != nil {
			errl.Println("Error reading domain list from:", fpath, err)
			return
		}
		if domain == "" {
			continue
		}
		lst = append(lst, strings.TrimSpace(domain))
	}
	return
}

func writeDomainList(fpath string, lst []string) (err error) {
	tmpPath := path.Join(config.dir, "tmp-domain")
	f, err := os.Create(tmpPath)
	if err != nil {
		errl.Println("Error creating tmp domain list file:", err)
		return
	}

	sort.Sort(sort.StringSlice(lst))

	all := strings.Join(lst, "\n")
	f.WriteString(all)
	f.Close()

	if err = os.Rename(tmpPath, fpath); err != nil {
		errl.Printf("Error moving tmp domain list file to %s: %v\n", fpath, err)
	}
	return
}

func hostIsIP(host string) bool {
	return net.ParseIP(host) != nil
}

func host2Domain(host string) (domain string) {
	dotPos := strings.LastIndex(host, ".")
	if dotPos == -1 {
		return host // simple host name
	}
	// Find the 2nd last dot
	dotPos = strings.LastIndex(host[:dotPos], ".")
	if dotPos == -1 {
		return host
	}
	return host[dotPos+1:]
}
