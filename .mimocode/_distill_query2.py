import sqlite3
import json
import time

DB = r"C:\Users\even\.local\share\mimocode\mimocode.db"
conn = sqlite3.connect(DB)
cur = conn.cursor()

# 1. Schema of session table
print("=== SESSION SCHEMA ===")
cur.execute("PRAGMA table_info(session)")
for row in cur.fetchall():
    print(f"  {row}")

# 2. Schema of message table
print("\n=== MESSAGE SCHEMA ===")
cur.execute("PRAGMA table_info(message)")
for row in cur.fetchall():
    print(f"  {row}")

# 3. Schema of part table
print("\n=== PART SCHEMA ===")
cur.execute("PRAGMA table_info(part)")
for row in cur.fetchall():
    print(f"  {row}")

# 4. Schema of task table
print("\n=== TASK SCHEMA ===")
cur.execute("PRAGMA table_info(task)")
for row in cur.fetchall():
    print(f"  {row}")

# 5. Schema of project table
print("\n=== PROJECT SCHEMA ===")
cur.execute("PRAGMA table_info(project)")
for row in cur.fetchall():
    print(f"  {row}")

# 6. List sessions
print("\n=== SESSIONS ===")
cur.execute("SELECT * FROM session ORDER BY time_created DESC LIMIT 20")
cols = [d[0] for d in cur.description]
print(f"  Columns: {cols}")
for row in cur.fetchall():
    print(f"  {row}")

# 7. List projects
print("\n=== PROJECTS ===")
cur.execute("SELECT * FROM project")
for row in cur.fetchall():
    print(f"  {row}")

conn.close()
