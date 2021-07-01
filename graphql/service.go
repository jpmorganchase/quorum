// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package graphql

import (
	"encoding/json"
	"net/http"

	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/plugin/security"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/graph-gophers/graphql-go"
)

type handler struct {
	Schema *graphql.Schema
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var params struct {
		Query         string                 `json:"query"`
		OperationName string                 `json:"operationName"`
		Variables     map[string]interface{} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := h.Schema.Exec(r.Context(), params.Query, params.OperationName, params.Variables)
	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(response.Errors) > 0 {
		w.WriteHeader(http.StatusBadRequest)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)

}

// New constructs a new GraphQL service instance.
func New(stack *node.Node, backend ethapi.Backend, cors, vhosts []string) error {
	if backend == nil {
		panic("missing backend")
	}
	// check if http server with given endpoint exists and enable graphQL on it
	return newHandler(stack, backend, cors, vhosts)
}

// newHandler returns a new `http.Handler` that will answer GraphQL queries.
// It additionally exports an interactive query browser on the / endpoint.
func newHandler(stack *node.Node, backend ethapi.Backend, cors, vhosts []string) error {
	q := Resolver{backend}

	s, err := graphql.ParseSchema(schema, &q)
	if err != nil {
		return err
	}
	h := handler{Schema: s}
	// Quorum
	// we wrap the handler with security logic to support
	// auth/authz and multiple private states handling
	// as GraphQL handler is created before services start
	// so we need to defer the authManagerFunc creation in the later call.
	authManagerFunc := func() (security.AuthenticationManager, error) {
		// Obtain the authentication manager for the handler to deal with rpc security
		_, auth, err := stack.GetSecuritySupports()
		if err != nil {
			return nil, err
		}
		if auth == nil {
			return security.NewDisabledAuthenticationManager(), nil
		}
		return auth, err
	}
	handler := &secureHandler{
		authManagerFunc: authManagerFunc,
		isMultitenant:   stack.Config().EnableMultitenancy,
		protectedMethod: "graphql_*", // this follows JSON RPC convention using namespace graphql
		delegate:        node.NewHTTPHandlerStack(h, cors, vhosts),
	}
	// need to obtain eth service in order to know if MPS is enabled
	isMPS := false
	var ethereum *eth.Ethereum
	if err := stack.Lifecycle(&ethereum); err != nil {
		log.Warn("Eth service is not ready yet", "error", err)
	} else {
		isMPS = ethereum.BlockChain().Config().IsMPS
	}
	stack.RegisterHandler("GraphQL UI", "/graphql/ui", GraphiQL{
		authManagerFunc: authManagerFunc,
		isMPS:           isMPS,
	})
	stack.RegisterHandler("GraphQL", "/graphql", handler)
	stack.RegisterHandler("GraphQL", "/graphql/", handler)

	return nil
}

// Quorum
//
// secureHandler wraps around the http handler in order to perform rpc security
// and propagate the PSI into the request context.
type secureHandler struct {
	delegate        http.Handler
	protectedMethod string
	authManagerFunc security.AuthenticationManagerDeferFunc
	isMultitenant   bool
}

func (h *secureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	authManager, err := h.authManagerFunc()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	securityContext := rpc.WithIsMultitenant(r.Context(), h.isMultitenant)
	// authentication check
	securityContext = rpc.AuthenticateHttpRequest(securityContext, r, authManager)
	// authorization check
	securedCtx, err := rpc.SecureCall(&securityContextHolder{ctx: securityContext}, h.protectedMethod)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	h.delegate.ServeHTTP(w, r.WithContext(securedCtx))
}

// Quorum
// securityContextHolder stores a context so it can be retrieved later
// via rpc.SecurityContextResolver interface
type securityContextHolder struct {
	ctx rpc.SecurityContext
}

func (sh *securityContextHolder) Resolve() rpc.SecurityContext {
	return sh.ctx
}
