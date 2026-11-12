package server

import (
	"fmt"
	gotypes "go/types"
	"path"
	"sort"
	"strings"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// validateResourceRename checks spx resource namespaces before generating edits.
func (r *spxAnalysis) validateResourceRename(id resourceID, newName string) error {
	switch id := id.(type) {
	case SpxBackdropResourceID:
		if r.spxResourceSet.Backdrop(newName) != nil {
			return fmt.Errorf("backdrop resource %q already exists", newName)
		}
	case SpxSoundResourceID:
		if r.spxResourceSet.Sound(newName) != nil {
			return fmt.Errorf("sound resource %q already exists", newName)
		}
	case SpxSpriteResourceID:
		if r.spxResourceSet.Sprite(newName) != nil {
			return fmt.Errorf("sprite resource %q already exists", newName)
		}
	case SpxSpriteCostumeResourceID:
		sprite := r.spxResourceSet.Sprite(id.SpriteName)
		if sprite == nil {
			return fmt.Errorf("sprite resource %q not found", id.SpriteName)
		}
		if sprite.Costume(newName) != nil {
			return fmt.Errorf("sprite costume resource %q already exists", newName)
		}
	case SpxSpriteAnimationResourceID:
		sprite := r.spxResourceSet.Sprite(id.SpriteName)
		if sprite == nil {
			return fmt.Errorf("sprite resource %q not found", id.SpriteName)
		}
		if sprite.Animation(newName) != nil {
			return fmt.Errorf("sprite animation resource %q already exists", newName)
		}
	case SpxWidgetResourceID:
		if r.spxResourceSet.Widget(newName) != nil {
			return fmt.Errorf("widget resource %q already exists", newName)
		}
	default:
		return fmt.Errorf("unsupported spx resource type: %T", id)
	}
	return nil
}

// appendSpxSpriteTypeRenames adds source type references for renamed sprites.
// Aliases retain their own names and enclosing type expressions are not edited.
func (s *Server) appendSpxSpriteTypeRenames(proj *xgo.Project, result *spxAnalysis, renames map[resourceID]string, changes map[DocumentURI][]TextEdit) {
	info, _ := proj.TypeInfo()
	if info == nil {
		return
	}
	for ident, obj := range info.Uses {
		typeName, ok := obj.(*gotypes.TypeName)
		if !ok || typeName.IsAlias() || !result.hasSpxSpriteType(typeName.Type()) || ident.Implicit() {
			continue
		}
		newName, renamed := renames[SpxSpriteResourceID{SpriteName: typeName.Name()}]
		if !renamed {
			continue
		}
		file := sourceASTFile(proj, ident.Pos())
		if file == nil {
			continue
		}
		uri := s.toDocumentURI(proj.Fset.File(ident.Pos()).Name())
		changes[uri] = append(changes[uri], TextEdit{Range: resourceRange(proj, file, ident), NewText: newName})
	}
}

// renameSpxResources validates the complete batch and plans its edits together.
func (s *Server) renameSpxResources(proj *xgo.Project, result *spxAnalysis, params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
	if result.spxResourceSetErr != nil {
		return nil, fmt.Errorf("failed to load spx resources: %w", result.spxResourceSetErr)
	}
	renames := make(map[resourceID]string)
	type target struct {
		context XGoResourceContextURI
		name    string
	}
	targets := make(map[target]bool)
	for _, param := range params {
		id, err := ParseSpxResourceURI(param.Resource.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse spx resource URI: %w", err)
		}
		if newName, ok := renames[id]; ok {
			if newName != param.NewName {
				return nil, fmt.Errorf("conflicting renames for spx resource %q", id.URI())
			}
			continue
		}
		if err := result.validateResourceRename(id, param.NewName); err != nil {
			return nil, fmt.Errorf("failed to rename spx resource %q: %w", param.Resource.URI, err)
		}
		destination := target{id.ContextURI(), param.NewName}
		if targets[destination] {
			return nil, fmt.Errorf("conflicting rename target %q in %q", param.NewName, id.ContextURI())
		}
		targets[destination] = true
		renames[id] = param.NewName
	}
	changes, err := s.renameResourcesAtRefs(proj, result.resourceAnalysis, renames)
	if err != nil {
		return nil, err
	}
	s.appendSpxSpriteTypeRenames(proj, result, renames, changes)
	edit := &WorkspaceEdit{Changes: changes}
	if err := s.checkSpxSpriteRenameConflicts(proj, result, renames, edit); err != nil {
		return nil, err
	}
	return edit, nil
}

// checkSpxSpriteRenameConflicts previews classfile moves as well as source edits
// because the host renames sprite files after applying the returned edits.
func (s *Server) checkSpxSpriteRenameConflicts(proj *xgo.Project, result *spxAnalysis, renames map[resourceID]string, edit *WorkspaceEdit) error {
	info, _ := proj.TypeInfo()
	files := make(map[string]string)
	for filename := range proj.Files() {
		named := spxSpriteTypeForFile(proj, filename)
		if named == nil {
			continue
		}
		name := named.Obj().Name()
		newName, renamed := renames[SpxSpriteResourceID{SpriteName: name}]
		if !renamed || name == newName {
			continue
		}
		if obj := info.Pkg.Scope().Lookup(newName); obj != nil {
			return fmt.Errorf("sprite rename conflicts with package declaration %q", newName)
		}
		if !token.IsIdentifier(newName) {
			return fmt.Errorf("sprite rename conflicts with invalid class name %q", newName)
		}
		ext := modfile.ClassExt(filename)
		base := newName
		for _, work := range spxFrameworkForFile(proj, filename).Works {
			if work.Ext == ext {
				var ok bool
				base, ok = strings.CutPrefix(newName, work.Prefix)
				if !ok || base == "" {
					return fmt.Errorf("sprite rename conflicts with class prefix %q", work.Prefix)
				}
				break
			}
		}
		files[filename] = path.Join(path.Dir(filename), base+ext)
	}
	bindings := make(map[gotypes.Object]string)
	for obj := range result.spxSpriteResourceAutoBindings {
		if name, renamed := renames[SpxSpriteResourceID{SpriteName: obj.Name()}]; renamed && name != obj.Name() {
			if err := checkFieldRenameConflicts(info, obj, name); err != nil {
				return err
			}
			bindings[obj] = name
		}
	}
	if len(files) == 0 && len(bindings) == 0 {
		return nil
	}
	preview, err := s.newRenamePreview(proj, edit)
	if err != nil {
		return err
	}
	preview.renamedFiles = files
	for oldName, newName := range files {
		if err := preview.project.RenameFile(oldName, newName); err != nil {
			return fmt.Errorf("sprite rename conflicts with file %q: %w", newName, err)
		}
	}
	previewInfo, _ := preview.project.TypeInfo()
	preview.objects = make(map[gotypes.Object]gotypes.Object, len(files)+len(bindings))
	for filename, renamed := range files {
		before := spxSpriteTypeForFile(proj, filename)
		after := spxSpriteTypeForFile(preview.project, renamed)
		if after == nil || after.Obj().Name() != renames[SpxSpriteResourceID{SpriteName: before.Obj().Name()}] {
			return fmt.Errorf("sprite rename conflicts with classfile %q", renamed)
		}
		preview.objects[before.Obj()] = after.Obj()
	}
	if len(bindings) != 0 {
		file, _ := preview.project.ASTFile(result.mainSpxFile)
		game := classTypeForFile(preview.project, file)
		if game == nil {
			return fmt.Errorf("sprite rename conflicts with project class")
		}
		for obj, name := range bindings {
			renamed, _, _ := gotypes.LookupFieldOrMethod(game, true, previewInfo.Pkg, name)
			if renamed == nil {
				return fmt.Errorf("sprite rename conflicts with unresolved binding %q", name)
			}
			preview.objects[obj] = renamed
		}
	}
	before, err := sourceInfoForProject(proj)
	if err != nil {
		return err
	}
	// Sprite identifiers retain their bindings and conversion checks. Resource
	// constants replaced by literals need neither check for the removed syntax.
	preserved := make(map[token.Pos]bool)
	for obj, refs := range before.references {
		if preview.objects[obj] != nil {
			for _, ref := range refs {
				preserved[ref.ident.Pos()] = true
			}
		}
	}
	replaced := func(pos token.Pos) bool {
		if preserved[pos] {
			return false
		}
		file := proj.Fset.File(pos)
		offset := file.Offset(pos)
		edits := preview.offsets[file.Name()]
		i := sort.Search(len(edits), func(i int) bool { return edits[i].end > offset })
		return i < len(edits) && offset >= edits[i].start
	}
	filter := func(refs map[gotypes.Object][]sourceIdent) map[gotypes.Object][]sourceIdent {
		filtered := make(map[gotypes.Object][]sourceIdent, len(refs))
		for obj, refs := range refs {
			for _, ref := range refs {
				if replaced(ref.ident.Pos()) {
					continue
				}
				filtered[obj] = append(filtered[obj], ref)
			}
		}
		return filtered
	}
	conversions := renameConversions(proj)
	for pos := range conversions {
		if replaced(pos) {
			delete(conversions, pos)
		}
	}
	return preview.checkConflicts(proj, &sourceInfo{
		references: filter(before.references),
		kwargs:     filter(before.kwargs),
		highlights: filter(before.highlights),
	}, conversions)
}
