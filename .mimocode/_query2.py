import sqlite3, json

db = r'C:\Users\even\.local\share\mimocode\mimocode.db'
conn = sqlite3.connect(db)
c = conn.cursor()
sid = 'ses_067dc281fffekcL7go4BHgDW2T'
c.execute("SELECT title, time_created FROM session WHERE id = ?", (sid,))
s = c.fetchone()
print("Title:", s[0])
c.execute("SELECT COUNT(*) FROM message WHERE session_id = ?", (sid,))
print("Messages:", c.fetchone()[0])
c.execute("""
    SELECT p.id, json_extract(p.data, '$.type') as ptype, p.data
    FROM part p
    WHERE p.session_id = ? AND json_extract(p.data, '$.type') = 'text'
    ORDER BY p.time_created
""", (sid,))
for r in c.fetchall():
    d = json.loads(r[2])
    text = d.get('text', '')
    if len(text) > 100:
        print(f"\nTEXT [{r[0]}]:")
        print(text[:3000])
conn.close()
