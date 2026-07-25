import sqlite3
import json
import time

DB = r"C:\Users\even\.local\share\mimocode\mimocode.db"
conn = sqlite3.connect(DB)
cur = conn.cursor()

cutoff_30d = int((time.time() - 30*86400) * 1000)

# Check message data structure
print("=== MESSAGE DATA SAMPLE ===")
cur.execute("SELECT id, data FROM message WHERE data IS NOT NULL LIMIT 3")
for row in cur.fetchall():
    d = json.loads(row[1]) if row[1] else {}
    print(f"  {row[0]}: keys={list(d.keys())}")
    print(f"    role={d.get('role')}, content_type={type(d.get('content'))}")
    c = d.get('content', '')
    if isinstance(c, str):
        print(f"    content[:100]={c[:100]}")
    elif isinstance(c, list):
        print(f"    content is list, len={len(c)}, first item keys={list(c[0].keys()) if c else 'empty'}")

# Try extracting content differently
print("\n=== USER MESSAGES (all time, last 20) ===")
cur.execute("""
    SELECT m.session_id, m.time_created, m.data
    FROM message m
    WHERE json_extract(m.data, '$.role') = 'user'
    ORDER BY m.time_created DESC
    LIMIT 20
""")
for row in cur.fetchall():
    d = json.loads(row[2]) if row[2] else {}
    c = d.get('content', '')
    if isinstance(c, str):
        preview = c[:200]
    elif isinstance(c, list):
        texts = [item.get('text', '') for item in c if isinstance(item, dict)]
        preview = ' '.join(texts)[:200]
    else:
        preview = str(c)[:200]
    print(f"  [{row[0]}] {preview}")

# Assistant messages
print("\n=== ASSISTANT MESSAGES (last 20) ===")
cur.execute("""
    SELECT m.session_id, m.time_created, substr(m.data, 1, 300)
    FROM message m
    WHERE json_extract(m.data, '$.role') = 'assistant'
    ORDER BY m.time_created DESC
    LIMIT 20
""")
for row in cur.fetchall():
    print(f"  [{row[0]}] {row[2][:250]}")

# Repeated tool inputs - with null safety
print("\n=== REPEATED TOOL INPUTS (all time, n>1) ===")
cur.execute("""
    SELECT json_extract(p.data, '$.tool') as tool,
           json_extract(p.data, '$.state.input') as raw_input,
           count(*) as n,
           group_concat(distinct m.session_id) as sessions
    FROM message m
    JOIN part p ON p.message_id = m.id
    WHERE json_extract(m.data, '$.role') = 'assistant'
      AND json_extract(p.data, '$.type') = 'tool'
      AND json_extract(p.data, '$.state.input') IS NOT NULL
    GROUP BY tool, json_extract(p.data, '$.state.input')
    HAVING n > 1
    ORDER BY n DESC
    LIMIT 30
""")
for row in cur.fetchall():
    inp = str(row[1])[:180] if row[1] else "null"
    print(f"  [{row[2]}x] {row[0]}: {inp}")
    print(f"       sessions: {row[3]}")

# Tasks
print("\n=== TASKS ===")
cur.execute("""
    SELECT t.id, t.session_id, t.status, t.summary, t.created_at, t.ended_at
    FROM task t
    ORDER BY t.created_at DESC
    LIMIT 20
""")
for row in cur.fetchall():
    print(f"  {row[0]} | {row[1]} | {row[2]} | {(row[3] or '')[:120]} | created={row[4]}")

conn.close()
