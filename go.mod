module github.com/goplus/xgolsw

go 1.25.0

tool github.com/goplus/xgolsw/cmd/pkgdatagen

require (
	github.com/goplus/gogen v1.23.0-pre.3.0.20260414234848-6641c10c9d6f
	github.com/goplus/mod v0.20.2
	github.com/goplus/spx/v2 v2.0.0-pre.49
	github.com/goplus/xgo v1.7.0
	github.com/qiniu/x v1.17.0
	github.com/stretchr/testify v1.11.1
	golang.org/x/mod v0.34.0
	golang.org/x/sync v0.20.0
	golang.org/x/text v0.35.0
	golang.org/x/tools v0.43.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/goplus/spbase v0.1.0 // indirect
	github.com/petermattis/goid v0.0.0-20250721140440-ea1c0173183e // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/image v0.23.0 // indirect
	golang.org/x/mobile v0.0.0-20220518205345-8578da9835fd // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/goplus/spx/v2 => github.com/aofei/fork.goplus.spx/v2 v2.0.0-20260415065509-00ad362f80cf
	github.com/goplus/xgo => github.com/aofei/fork.goplus.xgo v0.0.0-20260415094016-ec0c6ccc5053
)
