import React from 'react'

const icons: Record<string, string> = {}
const modules = import.meta.glob('../assets/lang-icons/*.svg', { eager: true, query: '?url', import: 'default' }) as Record<string, string>
for (const [path, url] of Object.entries(modules)) {
  const key = path.split('/').pop()!.replace('.svg', '')
  icons[key] = url
}

function extToKey(filename: string): string {
  if (filename.endsWith('/')) return 'folder'
  const lower = filename.toLowerCase()
  const base = lower.split('/').pop() || lower
  // Special filenames
  const specials: Record<string, string> = {
    'dockerfile': 'dockerfile', 'docker-compose.yml': 'dockerfile', 'docker-compose.yaml': 'dockerfile',
    'makefile': 'sh', 'gnumakefile': 'sh',
    '.gitignore': 'git', '.gitattributes': 'git', '.gitmodules': 'git',
    'package.json': 'js', 'package-lock.json': 'lock',
    'yarn.lock': 'lock', 'pnpm-lock.yaml': 'lock', 'bun.lockb': 'lock',
    'go.mod': 'go', 'go.sum': 'go',
    'cargo.toml': 'rs', 'cargo.lock': 'lock',
    'tsconfig.json': 'ts', 'tsconfig.node.json': 'ts',
    '.env': 'env', '.env.local': 'env', '.env.production': 'env', '.env.development': 'env',
    'readme.md': 'md', 'readme': 'md', 'license': 'default', 'licence': 'default',
  }
  if (specials[base]) return specials[base]
  const dotIdx = base.lastIndexOf('.')
  if (dotIdx === -1) return 'default'
  const ext = base.slice(dotIdx + 1)
  const extMap: Record<string, string> = {
    'go': 'go', 'ts': 'ts', 'tsx': 'tsx', 'js': 'js', 'jsx': 'tsx', 'mjs': 'mjs', 'cjs': 'js',
    'py': 'py', 'pyw': 'py', 'rs': 'rs', 'rb': 'rb', 'java': 'java', 'kt': 'kt', 'kts': 'kt',
    'c': 'c', 'h': 'c', 'cpp': 'cpp', 'cc': 'cpp', 'cxx': 'cpp', 'hpp': 'cpp',
    'cs': 'cs', 'swift': 'swift', 'php': 'php', 'lua': 'lua', 'dart': 'dart',
    'html': 'html', 'htm': 'html', 'css': 'css', 'scss': 'scss', 'sass': 'scss', 'less': 'css',
    'json': 'json', 'jsonc': 'json', 'yaml': 'yaml', 'yml': 'yaml', 'toml': 'toml',
    'md': 'md', 'mdx': 'md', 'txt': 'default', 'pdf': 'default',
    'sh': 'sh', 'bash': 'bash', 'zsh': 'sh', 'fish': 'sh', 'ps1': 'sh',
    'sql': 'sql', 'wasm': 'wasm', 'vim': 'vim', 'xml': 'default',
    'png': 'default', 'jpg': 'default', 'jpeg': 'default', 'gif': 'default', 'svg': 'default', 'ico': 'default',
    'lock': 'lock', 'mod': 'go', 'sum': 'go',
    'vue': 'vue', 'nuxt': 'nuxt',
  }
  return extMap[ext] || 'default'
}

export function FileIcon({ filename, size = 16 }: { filename: string; size?: number }) {
  const key = extToKey(filename)
  const url = icons[key] || icons['default']
  if (!url) return <span style={{ width: size, height: size, display: 'inline-block' }} />
  return (
    <img
      src={url}
      width={size}
      height={size}
      style={{ display: 'inline-block', verticalAlign: 'middle', flexShrink: 0 }}
      alt=""
    />
  )
}
