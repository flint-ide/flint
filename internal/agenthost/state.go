package agenthost

import "slices"

// Load is what keeps the host alive: running terminals, chats that were not
// stopped, and open connections.
type Load struct {
	Terminals int
	Chats     int
	Clients   int
}

// Idle reports a host with nothing to serve and nobody to serve it to.
func (l Load) Idle() bool { return l.Terminals+l.Chats+l.Clients == 0 }

// Load counts what the host is busy with. A terminal whose shell exited, or a
// stopped chat, is kept only so a window can still read how it ended; it does
// not keep the host running.
func (s *Server) Load() Load {
	l := Load{Clients: s.Clients()}
	s.termsMu.Lock()
	for _, e := range s.termMap {
		if !e.isExited() {
			l.Terminals++
		}
	}
	s.termsMu.Unlock()
	s.chatsMu.Lock()
	for _, c := range s.chatMap {
		c.mu.Lock()
		if !c.stopped {
			l.Chats++
		}
		c.mu.Unlock()
	}
	s.chatsMu.Unlock()
	return l
}

// held lists the ids of the terminals and chats the host keeps, sorted and
// never nil, for hello: a window that attaches later finds its sessions there.
func (s *Server) held() (terminals, chats []string) {
	terminals, chats = []string{}, []string{}
	s.termsMu.Lock()
	for id := range s.termMap {
		terminals = append(terminals, id)
	}
	s.termsMu.Unlock()
	s.chatsMu.Lock()
	for id := range s.chatMap {
		chats = append(chats, id)
	}
	s.chatsMu.Unlock()
	slices.Sort(terminals)
	slices.Sort(chats)
	return terminals, chats
}

// changed tells the host mode that Load may have changed. It is called with
// no lock held and must be quick (the host mode only wakes its idle timer).
func (s *Server) changed() {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange()
	}
}
