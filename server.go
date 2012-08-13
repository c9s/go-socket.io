package socketio

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

var (
	uriRegexp = regexp.MustCompile(`^(.+?)/(1)(?:(/[^/]+)(/[^/]+))?/?$`)
)

type Config struct {
	HeartbeatTimeout int
	ClosingTimeout   int
	NewSessionID     func() string
	Transports       *TransportManager
	Authorize        func(*http.Request) bool
}

type SocketIOServer struct {
	mutex            sync.RWMutex
	heartbeatTimeout int
	closingTimeout   int
	authorize        func(*http.Request) bool
	newSessionId     func() string
	transports       *TransportManager
	sessions         map[string]*Session
	*EventEmitter
}

func NewSocketIOServer(config *Config) *SocketIOServer {
	server := new(SocketIOServer)
	if config != nil {
		if config.HeartbeatTimeout != 0 {
			server.heartbeatTimeout = config.HeartbeatTimeout
		} else {
			server.heartbeatTimeout = 15
		}
		if config.ClosingTimeout != 0 {
			server.closingTimeout = config.ClosingTimeout
		} else {
			server.closingTimeout = 10
		}
		if config.NewSessionID != nil {
			server.newSessionId = config.NewSessionID
		} else {
			server.newSessionId = NewSessionID
		}
		if config.Transports != nil {
			server.transports = config.Transports
		} else {
			server.transports = DefaultTransports
		}
		server.authorize = config.Authorize
	}
	server.sessions = make(map[string]*Session)
	return server
}

func (srv *SocketIOServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	pieces := uriRegexp.FindStringSubmatch(path)
	if pieces == nil {
		log.Printf("invalid uri: %s", r.URL)
	}
	transportId := pieces[2]
	sessionId := pieces[3]
	// connect
	if transportId == "" { // imply session==""
		srv.handShake(w, r)
		return
	}
	// open
	if srv.transports.Get(transportId) == nil {
		http.Error(w, "transport unsupported", 400)
		return
	}
	session := srv.getSession(sessionId)
	if session == nil {
		http.Error(w, "invalid session id", 400)
		return
	}
	session.serve(transportId, w, r)
}

func (srv *SocketIOServer) handShake(w http.ResponseWriter, r *http.Request) {
	if srv.authorize != nil {
		if ok := srv.authorize(r); !ok {
			http.Error(w, "", 401)
			return
		}
	}
	sessionId := NewSessionID()
	if sessionId == "" {
		http.Error(w, "", 503)
		return
	}
	transportNames := srv.transports.GetTransportNames()
	fmt.Fprintf(w, "%s:%d:%d:%s",
		sessionId,
		srv.heartbeatTimeout,
		srv.closingTimeout,
		strings.Join(transportNames, ":"))
	session := NewSession(srv, sessionId)
	srv.addSession(session)
	srv.emit("connect", nil, session.Of(""))
}

func (srv *SocketIOServer) addSession(ss *Session) {
	srv.mutex.Lock()
	defer srv.mutex.Unlock()
	srv.sessions[ss.SessionId] = ss
}

func (srv *SocketIOServer) removeSession(ss *Session) {
	srv.mutex.Lock()
	defer srv.mutex.Unlock()
	delete(srv.sessions, ss.SessionId)
}

func (srv *SocketIOServer) getSession(sessionId string) *Session {
	srv.mutex.RLock()
	defer srv.mutex.RUnlock()
	return srv.sessions[sessionId]
}