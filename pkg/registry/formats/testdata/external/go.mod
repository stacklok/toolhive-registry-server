module example.com/registry-formats-consumer

go 1.26.0

require github.com/stacklok/toolhive-registry-server v0.0.0

require (
	github.com/modelcontextprotocol/registry v1.8.1 // indirect
	github.com/stacklok/toolhive-core v0.0.43 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/stacklok/toolhive-registry-server => ../../../../..
