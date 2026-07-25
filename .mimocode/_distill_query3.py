import sqlite3
import json
import time

DB = r"C:\Users\even\.local\share\mimocode\mimocode.db"
conn = sqlite3.connect(DB)
cur = conn.cursor()

cutoff_30d = int((time.time() - 30*86400) * 1000)

# 1. Messages per session for current project
print("=== CURRENT PROJECT SESSIONS ===")
cur.execute("""
    SELECT s.id, s.title, s.time_created, count(m.id) as msg_count
    FROM session s
    LEFT JOIN message m ON m.session_id = s.id
    WHERE s.project_id = 'e9d1d8af-868b-45f9-8458-c57f096f3a7d'
    GROUP BY s.id
    ORDER BY s.time_created DESC
""")
for row in cur.fetchall():
    print(f"  {row[0]} | {row[1]} | {row[2]} | {row[3]} msgs")

# 2. Global project sessions (non-checkpoint-writer)
print("\n=== GLOBAL PROJECT SESSIONS (non-checkpoint-writer) ===")
cur.execute("""
    SELECT s.id, s.title, s.time_created, count(m.id) as msg_count
    FROM session s
    LEFT JOIN message m ON m.session_id = s.id
    WHERE s.project_id = 'global'
      AND s.parent_id IS NULL
      AND s.title NOT LIKE 'checkpoint-writer%'
    GROUP BY s.id
    ORDER BY s.time_created DESC
""")
for row in cur.fetchall():
    print(f"  {row[0]} | {row[1]} | {row[2]} | {row[3]} msgs")

# 3. All user messages across all sessions (last 30 days)
print("\n=== USER MESSAGES (last 30 days, top 30) ===")
cur.execute("""
    SELECT m.session_id, m.time_created, substr(json_extract(m.data, '$.content'), 1, 250)
    FROM message m
    WHERE json_extract(m.data, '$.role') = 'user'
      AND m.time_created > ?
    ORDER BY m.time_created DESC
    LIMIT 30
""", (cutoff_30d,))
for row in cur.fetchall():
    print(f"  [{row[0]}] {row[2][:200]}")

# 4. All user messages across all sessions (ALL TIME)
print("\n=== USER MESSAGES (all time, last 30) ===")
cur.execute("""
    SELECT m.session_id, m.time_created, substr(json_extract(m.data, '$.content'), 1, 250)
    FROM message m
    WHERE json_extract(m.data, '$.role') = 'user'
    ORDER BY m.time_created DESC
    LIMIT 30
""")
for row in cur.fetchall():
    print(f"  [{row[0]}] {row[2][:200]}")

# 5. Tools used per session
print("\n=== TOOLS PER SESSION (all time) ===")
cur.execute("""
    SELECT m.session_id, json_extract(p.data, '$.tool') as tool, count(*) as n
    FROM message m
    JOIN part p ON p.message_id = m.id
    WHERE json_extract(m.data, '$.role') = 'assistant'
      AND json_extract(p.data, '$.type') = 'tool'
    GROUP BY m.session_id, tool
    ORDER BY m.session_id, n DESC
""")
for row in cur.fetchall():
    print(f"  [{row[0]}] {row[1]}: {row[2]}")

# 6. Repeated tool inputs across ALL sessions
print("\n=== REPEATED TOOL INPUTS (all time, n>1) ===")
cur.execute("""
    SELECT json_extract(p.data, '$.tool') as tool,
           substr(json_extract(p.data, '$.state.input'), 1, 250) as input_preview,
           count(*) as n,
           group_concat(distinct m.session_id) as sessions
    FROM message m
    JOIN part p ON p.message_id = m.id
    WHERE json_extract(m.data, '$.role') = 'assistant'
      AND json_extract(p.data, '$.type') = 'tool'
    GROUP BY tool, input_preview
    HAVING n > 1
    ORDER BY n DESC
    LIMIT 30
""")
for row in cur.fetchall():
    print(f"  [{row[2]}x] {row[0]}: {row[1][:180]}")
    print(f"       sessions: {row[3]}")

# 7. Tasks with status and summary
print("\n=== TASKS ===")
cur.execute("""
    SELECT t.id, t.session_id, t.status, t.summary, t.created_at, t.ended_at
    FROM task t
    ORDER BY t.created_at DESC
    LIMIT 20
""")
for row in cur.fetchall():
    print(f"  {row[0]} | {row[1]} | {row[2]} | {row[3][:120]} | created={row[4]}")

# 8. Actor registry (subagents)
print("\n=== ACTOR REGISTRY ===")
try:
    cur.execute("SELECT * FROM actor_registry LIMIT 20")
    cols = [d[0] for d in cur.description]
    print(f"  Columns: {cols}")
    for row in cur.fetchall():
        print(f"  {row}")
except Exception as e:
    print(f"  Error: {e}")

# 9. Workflow runs
print("\n=== WORKFLOW RUNS ===")
try:
    cur.execute("PRAGMA table_info(workflow_run)")
    cols = [d[0] for d in cur.fetchall()]
    print(f"  Columns: {cols}")
    cur.execute("SELECT * FROM workflow_run LIMIT 10")
    for row in cur.fetchall():
        print(f"  {row}")
except Exception as e:
    print(f"  Error: {e}")

conn.close()
