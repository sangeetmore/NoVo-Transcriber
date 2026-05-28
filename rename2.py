import os
import re

files = [
    'app/src/App.tsx',
    'backend-go/internal/agent/orchestrator.go',
    'backend-go/internal/api/qa.go',
    'backend-go/internal/api/server.go',
    'backend-go/internal/api/session.go'
]

replacements = [
    (re.compile(r'NoteItAgent'), 'NoVoTranscriberAgent'),
    (re.compile(r'requireNoteItBridge'), 'requireNoVoTranscriberBridge')
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
