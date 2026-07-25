import sqlite3, json, sys

db_path = r'C:\Users\even\.local\share\mimocode\mimocode.db'
conn = sqlite3.connect(db_path)
c = conn.cursor()

# Get the main openclaw review session
sid = 'ses_067dc28d2ffePcraPpcVyca8Md'
c.execute("SELECT title, directory, time_created FROM session WHERE id = ?", (sid,))
s = c.fetchone()
print(f"Title: {s[0]}")
print(f"Dir: {s[1]}")

# Get all parts in order
c.execute("""
    SELECT p.id, p.message_id, json_extract(p.data, '$.type') as ptype, p.data
    FROM part p
    WHERE p.session_id = ?
    ORDER BY p.time_created
""", (sid,))

for r in c.fetchall():
    ptype = r[2]
    d = json.loads(r[3])
    if ptype == 'text':
        text = d.get('text', '')
        if len(text) > 150:
            print(f"\n=== TEXT [{r[0]}] msg={r[1]} ===")
            print(text[:4000])
    elif ptype == 'tool':
        tool = d.get('tool', '?')
        state = d.get('state', {})
        inp = str(state.get('input', ''))[:200]
        out = str(state.get('output', ''))
        if out and len(out) > 20:
            print(f"\n=== TOOL:{tool} [{r[0]}] msg={r[1]} ===")
            print(f"  IN: {inp}")
            print(f"  OUT: {out[:2000]}")

# Now get the last message in the session (the conclusion)
c.execute("""
    SELECT m.id
    FROM message m
    WHERE m.session_id = ? AND json_extract(m.data, '$.role') = 'assistant'
    ORDER BY m.time_created DESC
    LIMIT 1
""", (sid,))
last_msg = c.fetchone()
if last_msg:
    msg_id = last_msg[0]
    print(f"\n{'='*60}")
    print(f"LAST ASSISTANT MESSAGE: {msg_id}")
    print(f"{'='*60}")
    c.execute("""
        SELECT p.id, json_extract(p.data, '$.type') as ptype, p.data
        FROM part p
        WHERE p.message_id = ?
        ORDER BY p.time_created
    """, (msg_id,))
    for r in c.fetchall():
        d = json.loads(r[2])
        ptype = r[1]
        if ptype == 'text':
            text = d.get('text', '')
            if text.strip():
                print(f"TEXT: {text[:5000]}")
        elif ptype == 'tool':
            tool = d.get('tool', '?')
            state = d.get('state', {})
            out = str(state.get('output', ''))
            if out and len(out) > 20:
                print(f"TOOL:{tool}: {out[:3000]}")

conn.close()
