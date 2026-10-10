module github.com/akfaiz/quill/test/bench-libs

go 1.25

require (
	github.com/Masterminds/squirrel v1.5.4
	github.com/akfaiz/quill v0.0.0
	github.com/doug-martin/goqu/v9 v9.19.0
	github.com/huandu/go-sqlbuilder v1.44.0
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/huandu/go-clone v1.7.3 // indirect
	github.com/huandu/xstrings v1.4.0 // indirect
	github.com/lann/builder v0.0.0-20180802200727-47ae307949d0 // indirect
	github.com/lann/ps v0.0.0-20150810152359-62de8c46ede0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

replace github.com/akfaiz/quill => ../..
