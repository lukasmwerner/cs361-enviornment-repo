from flask import Flask
from flask import request
from flask import jsonify
import requests
import sqlite3

app = Flask(__name__)

db = sqlite3.connect('users.db', check_same_thread=False)

@app.route("/search/<user>/<database>")
def hello_world(user, database):
    query = request.args.get('query')
    cursor = db.cursor()
    cursor.execute(f"SELECT * FROM user_{user}_collection_{database} WHERE metadata LIKE ? or name LIKE ?", ('%' + query + '%', '%' + query + '%'))
    results = cursor.fetchall()

    search_results = set(results)

    headers = {"content-type": "application/json"}

    resp = requests.post(url = "http://localhost:8081/search", json={ "query": query }, headers=headers)
    data = resp.json()
    for result in data:
        cursor.execute(f"SELECT * FROM user_{user}_collection_{database} WHERE image_refs LIKE ?", ('%' + result['id'] + '%',))
        search_results.update(set(cursor.fetchall()))

    finished_results = []
    for result in search_results:
        finished_results.append({
            "id": result[0],
            "name": result[1],
            "metadata": result[2],
            "image_refs": str(result[3].decode("utf-8"))
        })

    return jsonify(finished_results)

if __name__ == "__main__":
    app.run(port=8083)
