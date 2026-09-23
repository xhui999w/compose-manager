import Editor, { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor/editor/editor.api.js'
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker'

type MonacoWorkerScope = { MonacoEnvironment: { getWorker(): Worker } }
;(self as unknown as MonacoWorkerScope).MonacoEnvironment = { getWorker: () => new EditorWorker() }
loader.config({ monaco })

if (!monaco.languages.getLanguages().some((language) => language.id === 'yaml')) {
  monaco.languages.register({ id: 'yaml', extensions: ['.yaml', '.yml'], aliases: ['YAML', 'yaml'] })
  monaco.languages.setMonarchTokensProvider('yaml', {
    tokenizer: {
      root: [
        [/^\s*#.*/, 'comment'],
        [/[&*][\w.-]+/, 'type.identifier'],
        [/!\S+/, 'type'],
        [/(^|\s)(true|false|null|yes|no|on|off)(?=\s|$)/i, 'keyword'],
        [/(^|\s)-?\d+(\.\d+)?(?=\s|$)/, 'number'],
        [/"([^"\\]|\\.)*"/, 'string'],
        [/'[^']*'/, 'string'],
        [/^[\t ]*[^\s:#][^:#]*?(?=\s*:)/, 'key'],
        [/[{}[\],]/, 'delimiter.bracket'],
        [/:/, 'delimiter'],
      ],
    },
  })
}

export default function MonacoEditor({ value, onChange, language = 'yaml', height = 'calc(100vh - 190px)' }: { value: string; onChange: (value: string) => void; language?: string; height?: string | number }) {
  return (
    <Editor
      height={height}
      language={language}
      value={value}
      onChange={(next) => onChange(next ?? '')}
      options={{ minimap: { enabled: false }, fontSize: 13, lineHeight: 21, automaticLayout: true, wordWrap: 'off', tabSize: 2, insertSpaces: true, formatOnPaste: true, scrollBeyondLastLine: false, find: { addExtraSpaceOnTop: false } }}
    />
  )
}
