#!/usr/bin/env python3
"""Zamijeni tok u goCOP GeoJSON-u detaljnom OSM waterway relacijom.

Oznake riječnih kilometara i ostale značajke u datoteci ostaju netaknute.
Relacija se očekuje u redoslijedu toka, s neprekinutim članovima u ulozi
``main_stream``.
"""

import argparse
import json
import math
import pathlib
import urllib.parse
import urllib.request


OVERPASS = "https://overpass.kumi.systems/api/interpreter"


def preuzmi(relation_id: int) -> dict:
    query = f"[out:json][timeout:900];relation({relation_id});out geom;"
    request = urllib.request.Request(
        OVERPASS,
        data=urllib.parse.urlencode({"data": query}).encode(),
        headers={"User-Agent": "goCOP geometry updater/1.0"},
    )
    with urllib.request.urlopen(request, timeout=950) as response:
        return json.load(response)


def udaljenost_km(a: list[float], b: list[float]) -> float:
    srednja = math.radians((a[1] + b[1]) / 2)
    dx = (a[0] - b[0]) * math.cos(srednja)
    dy = a[1] - b[1]
    return 111.0 * math.hypot(dx, dy)


def slozi_tok(overpass: dict, relation_id: int) -> list[list[float]]:
    elementi = overpass.get("elements", [])
    relacije = [e for e in elementi if e.get("type") == "relation" and e.get("id") == relation_id]
    if len(relacije) != 1:
        raise ValueError(f"očekivana je jedna OSM relacija {relation_id}, dobiveno {len(relacije)}")

    clanovi = [m for m in relacije[0].get("members", []) if m.get("role") == "main_stream"]
    if not clanovi:
        raise ValueError("relacija nema članove main_stream")

    # Overpassov ``out geom`` nosi koordinate izravno u članu relacije. OSM
    # API ``relation/<id>/full.json`` umjesto toga vrati načine i njihove
    # čvorove kao zasebne elemente. Podržavamo oba oblika: izravni OSM API je
    # pouzdanija rezerva kad Overpass za veliku rijeku istekne.
    nacini = {e["id"]: e for e in elementi if e.get("type") == "way"}
    cvorovi = {
        e["id"]: [float(e["lon"]), float(e["lat"])]
        for e in elementi
        if e.get("type") == "node" and "lon" in e and "lat" in e
    }

    tok: list[list[float]] = []
    for clan in clanovi:
        geometrija = clan.get("geometry") or []
        if geometrija:
            dio = [[float(p["lon"]), float(p["lat"])] for p in geometrija]
        else:
            nacin = nacini.get(clan.get("ref"), {})
            try:
                dio = [cvorovi[node_id] for node_id in nacin.get("nodes", [])]
            except KeyError as missing:
                raise ValueError(
                    f"OSM član {clan.get('ref')} nema koordinate čvora {missing.args[0]}"
                ) from missing
        if len(dio) < 2:
            raise ValueError(f"OSM član {clan.get('ref')} nema potpunu geometriju")
        if tok:
            razmak = udaljenost_km(tok[-1], dio[0])
            if razmak > 0.01:
                raise ValueError(
                    f"prekid od {razmak:.3f} km prije OSM člana {clan.get('ref')}; "
                    "relaciju treba pregledati prije uvoza"
                )
            dio = dio[1:]
        tok.extend(dio)
    return tok


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--relation", type=int, required=True, help="OSM ID waterway relacije")
    parser.add_argument("--file", type=pathlib.Path, required=True, help="goCOP GeoJSON datoteka")
    parser.add_argument("--input", type=pathlib.Path, help="već preuzet Overpass JSON")
    args = parser.parse_args()

    with args.file.open(encoding="utf-8") as source:
        geojson = json.load(source)
    if not geojson.get("features") or geojson["features"][0].get("geometry", {}).get("type") != "LineString":
        raise ValueError("prva GeoJSON značajka mora biti LineString toka")

    if args.input:
        with args.input.open(encoding="utf-8") as source:
            overpass = json.load(source)
    else:
        overpass = preuzmi(args.relation)

    tok = slozi_tok(overpass, args.relation)
    geojson["features"][0]["geometry"]["coordinates"] = tok
    properties = geojson["features"][0].setdefault("properties", {})
    properties["izvor_geometrije"] = "OpenStreetMap"
    properties["osm_relation"] = args.relation

    temporary = args.file.with_suffix(args.file.suffix + ".tmp")
    with temporary.open("w", encoding="utf-8") as target:
        json.dump(geojson, target, ensure_ascii=False, indent=2)
        target.write("\n")
    temporary.replace(args.file)

    print(
        f"{args.file}: {len(tok)} točaka, "
        f"od {tok[0][0]:.6f},{tok[0][1]:.6f} do {tok[-1][0]:.6f},{tok[-1][1]:.6f}"
    )


if __name__ == "__main__":
    main()
