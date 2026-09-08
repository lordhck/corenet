package daemon

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"corenet/internal/registry"
	"corenet/pkg/protocol"
)

// API serves the CoreNet 0.1 HTTP APIs.
//
// The same handlers back two listeners: the control socket, which is local
// and may write, and the peer listener, which is on the network and is
// strictly read-only.
type API struct {
	Dir    Directory
	Status func() protocol.Status
	Info   func() protocol.Info
	Nodes  func() []protocol.Node
	Logger *log.Logger
}

// ControlMux is the full API, served on the local Unix socket.
func (a *API) ControlMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", a.handleStatus)
	mux.HandleFunc("GET /v1/services", a.handleServices(a.Dir.List, true))
	mux.HandleFunc("POST /v1/services", a.handleAddService)
	mux.HandleFunc("DELETE /v1/services/{name}", a.handleRemoveService)
	mux.HandleFunc("GET /v1/resolve", a.handleResolve)
	mux.HandleFunc("GET /v1/nodes", a.handleNodes)
	mux.HandleFunc("GET /v1/nodes/{id}", a.handleNode)
	mux.HandleFunc("/", a.handleNotFound)
	return mux
}

// PeerMux is what other nodes may ask this one. It advertises only the
// services this node serves itself, and accepts no writes.
func (a *API) PeerMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", a.handleInfo)
	mux.HandleFunc("GET /v1/services", a.handleServices(a.Dir.ListLocal, false))
	mux.HandleFunc("GET /v1/resolve", a.handleResolve)
	mux.HandleFunc("/", a.handleNotFound)
	return mux
}

func (a *API) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.Status())
}

func (a *API) handleInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.Info())
}

// handleServices lists services. On the control socket it also reports the
// names this node knows but refuses to route; the peer listener never does,
// since a name a node cannot route is not a name it can offer.
func (a *API) handleServices(list func() []protocol.Service, withConflicts bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		services := list()
		if services == nil {
			services = []protocol.Service{}
		}
		body := protocol.ServiceList{Services: services}
		if withConflicts {
			body.Conflicts = a.Dir.Conflicts()
		}
		writeJSON(w, http.StatusOK, body)
	}
}

func (a *API) handleAddService(w http.ResponseWriter, r *http.Request) {
	var svc protocol.Service
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&svc); err != nil {
		writeError(w, http.StatusBadRequest, protocol.CodeBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	added, err := a.Dir.Add(svc)
	switch {
	case errors.Is(err, registry.ErrConflict):
		writeError(w, http.StatusConflict, protocol.CodeConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, protocol.CodeBadRequest, err.Error())
	default:
		a.Logger.Printf("registered %s -> %s", added.Name, added.Addr())
		writeJSON(w, http.StatusCreated, added)
	}
}

func (a *API) handleRemoveService(w http.ResponseWriter, r *http.Request) {
	name := protocol.NormalizeName(r.PathValue("name"))
	err := a.Dir.Remove(name)
	switch {
	case errors.Is(err, registry.ErrNotFound):
		writeError(w, http.StatusNotFound, protocol.CodeNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusConflict, protocol.CodeConflict, err.Error())
	default:
		a.Logger.Printf("removed %s", name)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) handleResolve(w http.ResponseWriter, r *http.Request) {
	name := protocol.NormalizeName(r.URL.Query().Get("name"))
	if err := protocol.ValidateName(name); err != nil {
		writeError(w, http.StatusBadRequest, protocol.CodeBadRequest, err.Error())
		return
	}
	svc, ok := a.Dir.Lookup(name)
	if !ok {
		if conflict, conflicted := a.Dir.Conflict(name); conflicted {
			writeError(w, http.StatusConflict, protocol.CodeConflict,
				name+" is not routed: "+conflict.Reason)
			return
		}
		writeError(w, http.StatusNotFound, protocol.CodeNotFound, "unknown service: "+name)
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

func (a *API) handleNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, protocol.NodeList{Nodes: a.Nodes()})
}

func (a *API) handleNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, node := range a.Nodes() {
		if node.ID == id || node.Endpoint == id {
			writeJSON(w, http.StatusOK, node)
			return
		}
	}
	writeError(w, http.StatusNotFound, protocol.CodeNotFound, "unknown node: "+id)
}

func (a *API) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, protocol.CodeNotFound, "no such endpoint: "+r.URL.Path)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, protocol.ErrorResponse{
		Error: protocol.ErrorBody{Code: code, Message: message},
	})
}
