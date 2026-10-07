import psycopg2

conn = psycopg2.connect(
    dbname="qa2a",
    user="admin",
    password="!123Maxim.!",
    host="localhost",
    port=5433
)
cur = conn.cursor()

cur.execute("SELECT COUNT(*) FROM leads_restaurants")
total = cur.fetchone()[0]

cur.execute("SELECT COUNT(*) FROM leads_restaurants WHERE two_gis_id LIKE '7000000100%'")
mock_count = cur.fetchone()[0]

cur.execute("SELECT COUNT(*) FROM leads_restaurants WHERE legal_type = 'OOO'")
ooo_count = cur.fetchone()[0]

cur.execute("SELECT COUNT(*) FROM leads_restaurants WHERE legal_type = 'IP'")
ip_count = cur.fetchone()[0]

cur.execute("SELECT COUNT(*) FROM leads_restaurants WHERE legal_type = 'OTHER'")
other_count = cur.fetchone()[0]

print(f"Total: {total}, Mock seed: {mock_count}, OOO: {ooo_count}, IP: {ip_count}, OTHER: {other_count}")

cur.execute("SELECT name, legal_name, legal_type FROM leads_restaurants WHERE legal_type IN ('OOO', 'IP') LIMIT 10")
print("Real OOO/IP samples:")
for row in cur.fetchall():
    print(" ", row)

conn.close()
