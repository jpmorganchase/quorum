// generate stub/mock files for plugins, documentation and unit tests for plugin interfaces defined in .proto files
//
// need to install:
//  - protoc: 3.9.0+
//  - protoc-gen-go: 1.3.2+
//  - protoc-gen-doc: `go get -u github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc`
//  - mockgen: `go get -u github.com/golang/mock/mockgen`
//  - goimports: `go get -u golang.org/x/tools/cmd/goimports`
//
// go to terminal and run `go generate` from this directory

// generate stubs
//go:generate protoc -I . -I ../../vendor --go_out=plugins=grpc:. init.proto helloworld.proto

// generate mocks for unit testing
//go:generate mockgen -package mock_proto -destination mock_proto/mock_initializer.go -source init.pb.go
//go:generate mockgen -package mock_proto -destination mock_proto/mock_helloworld.go  -source helloworld.pb.go

// fix fmt
//go:generate goimports -w ./

// generate documentation
//go:generate protoc -I . -I ../../vendor --doc_out=docs.markdown.tmpl,init_interface.md:../../docs/PluggableArchitecture/Plugins/ init.proto
//go:generate protoc -I . -I ../../vendor --doc_out=docs.markdown.tmpl,interface.md:../../docs/PluggableArchitecture/Plugins/helloworld/ helloworld.proto

package proto