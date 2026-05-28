import os
import re

files = [
    'README.md',
    'app/electron/main.js',
    'app/electron/preload.cjs',
    'app/electron/preload.js',
    'app/package.json',
    'app/src/App.tsx',
    'app/src/vite-env.d.ts',
    'backend-go/internal/adapters/notion.go',
    'backend-go/internal/agent/classifier.go',
    'backend-go/internal/agent/curator.go',
    'backend-go/internal/agent/orchestrator.go',
    'backend-go/internal/api/qa.go',
    'backend-go/internal/api/server.go',
    'backend-go/internal/api/session.go',
    # Include Python backend just in case
    'backend/app/__init__.py',
    'backend/app/adapters/notion_writer.py',
    'backend/app/adapters/videodb_client.py',
    'backend/app/agent/__init__.py',
    'backend/app/agent/classifier.py',
    'backend/app/agent/curator.py',
    'backend/app/agent/event_designer.py',
    'backend/app/agent/orchestrator.py',
    'backend/app/routes/session.py'
]

replacements = [
    (re.compile(r'\bNote It\b'), 'NoVo Transcriber'),
    (re.compile(r'\bnote it\b'), 'novo transcriber'),
    (re.compile(r'\bnote-it\b'), 'novo-transcriber'),
    (re.compile(r'\bNoteIt\b'), 'NoVoTranscriber'),
    (re.compile(r'\bnoteit\b'), 'novotranscriber'),
    (re.compile(r'\bNOTEIT_\b'), 'NOVO_')
]

for f in files:
    try:
        with open(f, 'r') as file:
            content = file.read()
        
        original = content
        for regex, replacement in replacements:
            content = regex.sub(replacement, content)
        
        if content != original:
            with open(f, 'w') as file:
                file.write(content)
            print(f"Updated {f}")
    except Exception as e:
        print(f"Skipped {f}: {e}")
