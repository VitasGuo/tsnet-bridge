import sqlite3
import json
import time

DB = r"C:\Users\even\.local\share\mimocode\mimocode.db"
conn = sqlite3.connect(DB)
cur = conn.cursor()

# 1. List tables
cur.execute("SELECT name FROM sqlite_master WHERE type='table'")
tables = [r[0] for r in cur.fetchall()]
print("=== TABLES ===")
print(tables)

# 2. List sessions (all, ordered by time desc)
print("\n=== SESSIONS ===")
cur.execute("SELECT id, time_created, data FROM session ORDER BY time_created DESC")
for row in cur.fetchall():
    sid, tc, data = row
    d = json.loads(data) if data else {}
    title = d.get("title", "")
    directory = d.get("directory", "")
    print(f"{sid} | {tc} | {title} | {directory}")

# 3. Count messages per session in last 30 days
cutoff = int((time.time() - 30*86400) * 1000)
print(f"\n=== MESSAGES (last 30 days, cutoff={cutoff}) ===")
cur.execute("SELECT session_id, count(*) FROM message WHERE time_created > ? GROUP BY session_id ORDER BY count(*) DESC", (cutoff,))
for row in cur.fetchall():
    print(f"  {row[0]}: {row[1]} messages")

# 4. All messages regardless of time
print("\n=== ALL MESSAGES BY SESSION ===")
cur.execute("SELECT session_id, count(*) FROM message GROUP BY session_id ORDER BY count(*) DESC")
for row in cur.fetchall():
    print(f"  {row[0]}: {row[1]} messages")

# 5. Part types breakdown
print("\n=== PART TYPES ===")
cur.execute("SELECT json_extract(data, '$.type'), count(*) FROM part GROUP BY json_extract(data, '$.type') ORDER BY count(*) DESC")
for row in cur.fetchall():
    print(f"  {row[0]}: {row[1]}")

# 6. Most used tools (recent 30 days)
print("\n=== TOOLS USED (last 30 days) ===")
cur.execute("""
    SELECT json_extract(p.data, '$.tool') as tool, count(*) as n
    FROM message m
    JOIN part p ON p.message_id = m.id
    WHERE json_extract(m.data, '$.role') = 'assistant'
      AND json_extract(p.data, '$.type') = 'tool'
      AND m.time_created > ?
    GROUP BY tool
    ORDER BY n DESC
    LIMIT 30
""", (cutoff,))
for row in cur.fetchall():
    print(f"  {row[0]}: {row[1]}")

# 7. Most used tools (all time)
print("\n=== TOOLS USED (all time) ===")
cur.execute("""
    SELECT json_extract(p.data, '$.tool') as tool, count(*) as n
    FROM message m
    JOIN part p ON p.message_id = m.id
    WHERE json_extract(m.data, '$.role') = 'assistant'
      AND json_extract(p.data, '$.type') = 'tool'
    GROUP BY tool
    ORDER BY n DESC
    LIMIT 30
""")
for row in cur.fetchall():
    print(f"  {row[0]}: {row[1]}")

# 8. Repeated tool input patterns (all time, since sessions are old)
print("\n=== REPEATED TOOL INPUTS (all time, top 30) ===")
cur.execute("""
    SELECT json_extract(p.data, '$.tool') as tool,
           substr(json_extract(p.data, '$.state.input'), 1, 200) as input_preview,
           count(*) as n
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
    print(f"  [{row[2]}x] {row[0]}: {row[1][:150]}")

# 9. User messages containing repeated keywords
print("\n=== USER MESSAGES WITH REPEATED KEYWORDS ===")
for kw in ["again", "every time", "like last time", "the usual", "repeat", "same as before", "again", "按照之前", "像上次", " usual"]:
    cur.execute("""
        SELECT m.session_id, substr(json_extract(m.data, '$.content'), 1, 200)
        FROM message m
        WHERE json_extract(m.data, '$.role') = 'user'
          AND json_extract(m.data, '$.content') LIKE ?
        LIMIT 5
    """, (f"%{kw}%",))
    rows = cur.fetchall()
    if rows:
        print(f"\n  Keyword '{kw}':")
        for row in rows:
            print(f"    [{row[0]}] {row[1][:120]}")

# 10. Files edited multiple times
print("\n=== FILES EDITED MULTIPLE TIMES ===")
cur.execute("""
    SELECT json_extract(p.data, '$.state.input') as inp, count(*) as n
    FROM message m
    JOIN part p ON p.message_id = m.id
    WHERE json_extract(m.data, '$.role') = 'assistant'
      AND json_extract(p.data, '$.tool') IN ('edit', 'write')
      AND json_extract(p.data, '$.type') = 'tool'
    GROUP BY inp
    HAVING n > 1
    ORDER BY n DESC
    LIMIT 20
""")
for row in cur.fetchall():
    print(f"  [{row[1]}x] {str(row[0])[:200]}")

# 11. Task table
print("\n=== TASKS ===")
try:
    cur.execute("SELECT id, session_id, time_created, time_modified, data FROM task ORDER BY time_modified DESC LIMIT 20")
    for row in cur.fetchall():
        d = json.loads(row[4]) if row[4] else {}
        print(f"  {row[0]} | {row[1]} | status={d.get('status','')} | title={str(d.get('title',''))[:80]}")
except Exception as e:
    print(f"  Error: {e}")

conn.close()
