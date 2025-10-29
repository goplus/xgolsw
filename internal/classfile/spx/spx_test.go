package spx

import "github.com/goplus/xgolsw/xgo"

func newTestProject(files map[string]string, feats uint) *xgo.Project {
	projFiles := make(map[string]*xgo.File, len(files))
	for name, content := range files {
		projFiles[name] = &xgo.File{Content: []byte(content)}
	}
	proj := xgo.NewProject(nil, projFiles, feats)
	proj.PkgPath = "main"
	return proj
}

func defaultProjectFiles() map[string]string {
	return map[string]string{
		"main.spx": `
Hero.
run "assets", {Title: "My Game"}
`,
		"Hero.spx": `
onStart => {
	Hero.say "Hello"
}
`,
		"assets/index.json": `{
	"backdrops": [{"name": "Sky", "path": "backdrops/sky.png"}],
	"zorder": [{"name": "StartButton", "type": "button", "label": "Start", "val": "start"}]
}`,
		"assets/sprites/Hero/index.json": `{
	"name": "Hero",
	"costumes": [
		{"name": "Idle", "path": "sprites/Hero/idle.png"},
		{"name": "Run", "path": "sprites/Hero/run.png"}
	],
	"fAnimations": {
		"RunLoop": {"frameFrom": "Run", "frameTo": "Run"}
	},
	"defaultAnimation": "RunLoop"
}`,
		"assets/sounds/Click/index.json": `{"name": "Click", "path": "sounds/Click/click.wav"}`,
	}
}
