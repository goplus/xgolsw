# xgolsw

[![Test](https://github.com/goplus/xgolsw/actions/workflows/test.yaml/badge.svg)](https://github.com/goplus/xgolsw/actions/workflows/test.yaml)
[![codecov](https://codecov.io/gh/goplus/xgolsw/branch/main/graph/badge.svg)](https://codecov.io/gh/goplus/xgolsw)
[![Go Report Card](https://goreportcard.com/badge/github.com/goplus/xgolsw)](https://goreportcard.com/report/github.com/goplus/xgolsw)
[![Go Reference](https://pkg.go.dev/badge/github.com/goplus/xgolsw.svg)](https://pkg.go.dev/github.com/goplus/xgolsw)

A lightweight XGo language server that runs in the browser using WebAssembly.

This project follows the
[Language Server Protocol (LSP)](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/)
using [JSON-RPC 2.0](https://www.jsonrpc.org/specification) for message exchange. However, unlike traditional LSP
implementations that require a network transport layer, this project operates directly in the browser's memory space
through its API interfaces.

## Difference between [`xgols`](https://github.com/goplus/xgols) and `xgolsw`

- `xgols` runs locally, while `xgolsw` runs in the browser using WebAssembly.
- `xgols` supports a workspace (multiple projects), while `xgolsw` supports a single project.
- `xgols` supports mixed programming of Go and XGo, while `xgolsw` only supports a pure XGo project.

## Building from source

1. [Optional] Generate required package data:

  ```bash
  go generate ./internal/pkgdata
  ```

2. Build the project:

  ```bash
  GOOS=js GOARCH=wasm go build -trimpath -ldflags "-s -w" -o xgolsw.wasm
  ```

## Usage

This project is a standard Go WebAssembly module. You can use it like any other Go WebAssembly modules in your web
applications.

For detailed API references, please check the [index.d.ts](index.d.ts) file.

## Supported LSP methods

| Category | Method | Purpose & Explanation |
|----------|--------|-----------------------|
| **Lifecycle Management** |||
|| [`initialize`](https://microsoft.github.io/language-server-protocol/specifications/base/0.9/specification/#initialize) | Performs initial handshake, establishes server capabilities and client configuration. |
|| [`initialized`](https://microsoft.github.io/language-server-protocol/specifications/base/0.9/specification/#initialized) | Marks completion of initialization process, enabling request processing. |
|| [`shutdown`](https://microsoft.github.io/language-server-protocol/specifications/base/0.9/specification/#shutdown) | *Protocol conformance only.* |
|| [`exit`](https://microsoft.github.io/language-server-protocol/specifications/base/0.9/specification/#exit) | *Protocol conformance only.* |
| **Document Synchronization** |||
|| [`textDocument/didOpen`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_didOpen) | Registers new document in server state and triggers initial diagnostics. |
|| [`textDocument/didChange`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_didChange) | Synchronizes document content changes between client and server. |
|| [`textDocument/didSave`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_didSave) | Processes document save events and triggers related operations. |
|| [`textDocument/didClose`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_didClose) | Removes document from server state and cleans up resources. |
| **Code Intelligence** |||
|| [`textDocument/hover`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_hover) | Shows types and documentation at cursor position. |
|| [`textDocument/completion`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_completion) | Generates context-aware code suggestions. |
|| [`textDocument/signatureHelp`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_signatureHelp) | Shows function/method signature information. |
| **Symbols & Navigation** |||
|| [`textDocument/declaration`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_declaration) | Finds symbol declarations. |
|| [`textDocument/definition`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_definition) | Locates symbol definitions across workspace. |
|| [`textDocument/typeDefinition`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_typeDefinition) | Navigates to type definitions of variables/fields. |
|| [`textDocument/implementation`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_implementation) | Locates implementations. |
|| [`textDocument/references`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_references) | Finds all references of a symbol. |
|| [`textDocument/documentHighlight`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_documentHighlight) | Highlights other occurrences of selected symbol. |
|| [`textDocument/documentLink`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_documentLink) | Provides clickable links within document content. |
| **Code Quality** |||
|| [`textDocument/publishDiagnostics`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_publishDiagnostics) | Reports code errors and warnings in real-time. |
|| [`textDocument/diagnostic`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_diagnostic) | Pulls diagnostics for documents on request (pull model). |
|| [`workspace/diagnostic`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_diagnostic) | Pulls diagnostics for all workspace documents on request. |
| **Code Modification** |||
|| [`textDocument/formatting`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_formatting) | Applies standardized formatting rules to document. |
|| [`textDocument/prepareRename`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_prepareRename) | Validates renaming possibility and returns valid range for the operation. |
|| [`textDocument/rename`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_rename) | Performs consistent symbol renaming across workspace. |
| **Semantic Features** |||
|| [`textDocument/semanticTokens/full`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#semanticTokens_fullRequest) | Provides semantic coloring for whole document. |
|| [`textDocument/inlayHint`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_inlayHint) | Provides inline hints such as parameter names and type annotations. |
| **Other** |||
|| [`workspace/executeCommand`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_executeCommand) | Executes [predefined commands](#predefined-commands) for workspace-specific operations. |

## Predefined commands

### XGo resource renaming

The `xgo.renameResources` command enables renaming of XGo resources referenced by string literals (e.g.,
`play "explosion"`) across the workspace.

*Request:*

- method: [`workspace/executeCommand`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_executeCommand)
- params: `XGoRenameResourcesExecuteCommandParams` defined as follows:

```typescript
type XGoRenameResourcesExecuteCommandParams = Omit<ExecuteCommandParams, 'command' | 'arguments'> & {
  /**
   * The identifier of the actual command handler.
   */
  command: 'xgo.renameResources'

  /**
   * Arguments that the command should be invoked with.
   */
  arguments: XGoRenameResourceParams[]
}
```

```typescript
/**
 * Parameters to rename an XGo resource in the workspace.
 */
interface XGoRenameResourceParams {
  /**
   * The XGo resource to rename.
   */
  resource: XGoResourceIdentifier

  /**
   * The new name of the XGo resource.
   */
  newName: string
}
```

```typescript
/**
 * The XGo resource's identifier.
 */
interface XGoResourceIdentifier {
  /**
   * The XGo resource's URI.
   */
  uri: XGoResourceUri
}
```

```typescript
/**
 * The XGo resource's URI.
 *
 * For example:
 * - `spx://resources/sounds/MySound`
 * - `spx://resources/sprites/MySprite`
 * - `spx://resources/sprites/MySprite/costumes/MyCostume`
 * - `spx://resources/sprites/MySprite/animations/MyAnimation`
 * - `spx://resources/backdrops/MyBackdrop`
 * - `spx://resources/widgets/MyWidget`
 */
type XGoResourceUri = string
```

*Response:*

- result: [`WorkspaceEdit`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspaceEdit)
  | `null` describing the modification to the workspace. `null` should be treated the same as
  [`WorkspaceEdit`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspaceEdit)
with no changes (no change was required).
- error: code and message set when the rename operation cannot be performed for any reason.

### XGo input slots lookup

The `xgo.getInputSlots` command retrieves all modifiable items (XGo input slots) in a document, which can be used to
provide UI controls for assisting users with code modifications.

*Request:*

- method: [`workspace/executeCommand`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_executeCommand)
- params: `XGoGetInputSlotsExecuteCommandParams` defined as follows:

```typescript
type XGoGetInputSlotsExecuteCommandParams = Omit<ExecuteCommandParams, 'command' | 'arguments'> & {
  /**
   * The identifier of the actual command handler.
   */
  command: 'xgo.getInputSlots'

  /**
   * Arguments that the command should be invoked with.
   */
  arguments: [XGoGetInputSlotsParams]
}
```

```typescript
/**
 * Parameters to retrieve XGo input slots in a document.
 */
interface XGoGetInputSlotsParams {
  /**
   * The text document.
   */
  textDocument: TextDocumentIdentifier
}
```

*Response:*

- result: `XGoInputSlot[]` | `null` describing the XGo input slots found in the document. `null` indicates no XGo input
  slots were found.
- error: code and message set when XGo input slots cannot be retrieved for any reason.

```typescript
/**
 * The XGo input slot for a modifiable item in code.
 */
interface XGoInputSlot {
  /**
   * The document range of the XGo input slot.
   */
  range: Range

  /**
   * The kind of the XGo input slot.
   */
  kind: XGoInputSlotKind

  /**
   * The accepted inputs for the XGo input slot.
   */
  accept: XGoInputSlotAccept

  /**
   * The current input in the XGo input slot.
   */
  input: XGoInput

  /**
   * The available user-predefined identifiers.
   */
  predefinedNames: string[]
}
```

```typescript
/**
 * The kinds of XGo input slots.
 */
enum XGoInputSlotKind {
  /**
   * The slot accepts a value, which may be an in-place value or a predefined identifier.
   *
   * For example:
   * - `123` in `println 123`
   * - `name` in `println name`
   */
  Value = 'value',

  /**
   * The slot accepts an address, which must be a predefined identifier.
   *
   * For example:
   * - `x` in `x = 123`
   * - `y` in `x = y`
   */
  Address = 'address'
}
```

```typescript
/**
 * The accepted input for an XGo input slot.
 */
type XGoInputSlotAccept =
  | {
      /**
       * The input type accepted by the slot.
       */
      type:
        | XGoInputType.String
        | XGoInputType.Integer
        | XGoInputType.Decimal
        | XGoInputType.Boolean
        | XGoInputType.Unknown
        | XGoInputType.SpxDirection
        | XGoInputType.SpxLayerAction
        | XGoInputType.SpxDirAction
        | XGoInputType.SpxColor
        | XGoInputType.SpxEffectKind
        | XGoInputType.SpxKey
        | XGoInputType.SpxSpecialObj
        | XGoInputType.SpxRotationStyle
    }
  | {
      /**
       * The input type accepted by the slot.
       */
      type: XGoInputType.SpxResourceName

      /**
       * The resource context for the resource name input type.
       */
      resourceContext: XGoResourceContextUri
    }
```

```typescript
/**
 * The type of input for a slot.
 */
enum XGoInputType {
  /**
   * String values.
   */
  String = 'string',

  /**
   * Integer number values.
   */
  Integer = 'integer',

  /**
   * Decimal number values.
   */
  Decimal = 'decimal',

  /**
   * Boolean values.
   */
  Boolean = 'boolean',

  /**
   * Unknown type.
   */
  Unknown = 'unknown',

  /**
   * Resource name (`SpriteName`, `SoundName`, etc.) in spx.
   */
  SpxResourceName = 'spx-resource-name',

  /**
   * Direction values in spx.
   */
  SpxDirection = 'spx-direction',

  /**
   * layerAction values in spx.
   */
  SpxLayerAction = 'spx-layer-action',

  /**
   * dirAction values in spx.
   */
  SpxDirAction = 'spx-dir-action',

  /**
   * Color values in spx.
   */
  SpxColor = 'spx-color',

  /**
   * Effect kind values in spx.
   */
  SpxEffectKind = 'spx-effect-kind',

  /**
   * Keyboard key values in spx.
   */
  SpxKey = 'spx-key',

  /**
   * Special object values in spx.
   */
  SpxSpecialObj = 'spx-special-obj',

  /**
   * Rotation style values in spx.
   */
  SpxRotationStyle = 'spx-rotation-style'
}
```

```typescript
/**
 * The names of color constructors.
 */
type XGoInputTypeSpxColorConstructor = 'HSB' | 'HSBA'
```

```typescript
/**
 * The input value with type information.
 */
type XGoInputTypedValue =
  | { type: XGoInputType.String; value: string }
  | { type: XGoInputType.Integer; value: number }
  | { type: XGoInputType.Decimal; value: number }
  | { type: XGoInputType.Boolean; value: boolean }
  | { type: XGoInputType.Unknown; value: void }
  | { type: XGoInputType.SpxResourceName; value: XGoResourceUri }
  | { type: XGoInputType.SpxDirection; value: number }
  | { type: XGoInputType.SpxLayerAction; value: string }
  | { type: XGoInputType.SpxDirAction; value: string }
  | {
      type: XGoInputType.SpxColor
      value: {
        /**
         * Constructor for color.
         */
        constructor: XGoInputTypeSpxColorConstructor

        /**
         * Arguments passed to the constructor.
         */
        args: number[]
      }
    }
  | { type: XGoInputType.SpxEffectKind; value: string }
  | { type: XGoInputType.SpxKey; value: string }
  | { type: XGoInputType.SpxSpecialObj; value: string }
  | { type: XGoInputType.SpxRotationStyle; value: string }
```

```typescript
/**
 * The URI of the resource context.
 *
 * For example:
 * - `spx://resources/sprites`
 * - `spx://resources/sounds`
 * - `spx://resources/sprites/<sName>/costumes`
 */
type XGoResourceContextUri = string
```

```typescript
/**
 * Represents the current input in a slot.
 */
type XGoInput<T extends XGoInputTypedValue = XGoInputTypedValue> =
  | {
      /**
       * In-place value.
       *
       * For example:
       * - `"hello world"`
       * - `123`
       * - `true`
       * - spx `Left`
       * - spx `HSB(0,0,0)`
       */
      kind: XGoInputKind.InPlace

      /**
       * Type of the input.
       */
      type: T['type']

      /**
       * In-place value.
       */
      value: T['value']
    }
  | {
      /**
       * (Reference to) user predefined identifier.
       *
       * For example:
       * - var `costume1`
       * - const `name2`
       * - field `num3`
       */
      kind: XGoInputKind.Predefined

      /**
       * Type of the input.
       */
      type: T['type']

      /**
       * Name for user predefined identifer.
       */
      name: string
    }
```

```typescript
/**
 * The kind of input.
 */
enum XGoInputKind {
 /**
  * In-place value.
  *
   * For example:
   * - `"hello world"`
   * - `123`
   * - `true`
   * - spx `Left`
   * - spx `HSB(0,0,0)`
  */
  InPlace = 'in-place',

  /**
   * (Reference to) user predefined identifier.
   *
   * For example:
   * - var `costume1`
   * - const `name2`
   * - field `num3`
  */
  Predefined = 'predefined'
}
```

### XGo property lookup

The `xgo.getProperties` command retrieves properties (direct fields and auto-getter methods) for a target type or
instance (for example, `Game` or a sprite name).

*Request:*

- method: [`workspace/executeCommand`](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#workspace_executeCommand)
- params: `XGoGetPropertiesExecuteCommandParams` defined as follows:

```typescript
type XGoGetPropertiesExecuteCommandParams = Omit<ExecuteCommandParams, 'command' | 'arguments'> & {
  /**
   * The identifier of the actual command handler.
   */
  command: 'xgo.getProperties'

  /**
   * Arguments that the command should be invoked with.
   */
  arguments: [XGoGetPropertiesParams]
}
```

```typescript
/**
 * Parameters to retrieve properties for a target.
 */
interface XGoGetPropertiesParams {
  /**
   * The target name, for example `Game` or a specific sprite name.
   */
  target: string
}
```

*Response:*

- result: `XGoProperty[]` describing the properties found. An empty array means no properties were found.
- error: code and message set when properties cannot be retrieved for any reason.

```typescript
/**
 * A property of a target type.
 */
interface XGoProperty {
  /**
   * The property name.
   */
  name: string

  /**
   * The property type as a string.
   */
  type: string

  /**
   * The kind of property.
   */
  kind: 'field' | 'method'

  /**
   * Optional documentation for the property.
   */
  doc?: string
}
```

## Other JSON structures

### Document link data types

```typescript
/**
 * The data of an XGo resource reference DocumentLink.
 */
interface XGoResourceRefDocumentLinkData {
  /**
   * The kind of the XGo resource reference.
   */
  kind: XGoResourceRefKind
}
```

```typescript
/**
 * The kind of the XGo resource reference.
 *
 * - stringLiteral: String literal as a resource-reference, e.g., `play "explosion"`
 * - autoBindingReference: Reference for auto-binding variable as a resource-reference, e.g., `play explosion`
 * - constantReference: Reference for constant as a resource-reference, e.g., `play EXPLOSION` (`EXPLOSION` is a constant)
 */
type XGoResourceRefKind = 'stringLiteral' | 'autoBindingReference' | 'constantReference'
```

### Completion item data types

```typescript
/**
 * The data of a completion item.
 */
interface XGoCompletionItemData {
  /**
   * The corresponding definition of the completion item.
   */
  definition?: XGoDefinitionIdentifier
}
```

```typescript
/**
 * The identifier of a definition.
 */
interface XGoDefinitionIdentifier {
  /**
   * Full name of source package.
   * If not provided, it's assumed to be kind-statement.
   * If `main`, it's the current user package.
   *
   * For example:
   * - `fmt`
   * - `github.com/goplus/spx/v2`
   * - `main`
   */
  package?: string;

  /**
   * Exported name of the definition.
   * If not provided, it's assumed to be kind-package.
   *
   * For example:
   * - `Println`
   * - `Sprite`
   * - `Sprite.turn`
   * - `for_statement_with_single_condition`
   */
  name?: string;

  /**
   * Overload Identifier.
   */
  overloadId?: string;
}
```
