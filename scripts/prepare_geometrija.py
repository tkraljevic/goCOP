#!/usr/bin/env python3
"""
prepare_geometrija.py
Generates GeoJSON files with river polylines and calibrated river kilometers (rkm)
for Dunav, Drava, and Mura.
"""

import os
import sys
import json
import math
import urllib.request

CACHE_NE = "data/ne_10m_rivers_lake_centerlines.geojson"
NE_URL = "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_10m_rivers_lake_centerlines.geojson"

def haversine_km(lon1, lat1, lon2, lat2):
    R = 6371.0088
    dlat = math.radians(lat2 - lat1)
    dlon = math.radians(lon2 - lon1)
    a = math.sin(dlat/2)**2 + math.cos(math.radians(lat1))*math.cos(math.radians(lat2))*math.sin(dlon/2)**2
    return 2 * R * math.asin(math.sqrt(a))

def line_length(coords):
    total = 0.0
    for i in range(len(coords) - 1):
        total += haversine_km(coords[i][0], coords[i][1], coords[i+1][0], coords[i+1][1])
    return total

def ensure_ne_data():
    if not os.path.exists(CACHE_NE):
        print(f"Downloading Natural Earth rivers dataset to {CACHE_NE}...")
        req = urllib.request.Request(NE_URL, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req) as resp, open(CACHE_NE, "wb") as f:
            f.write(resp.read())
        print("Downloaded successfully.")

def interpolate_point(p1, p2, fraction):
    lon = p1[0] + (p2[0] - p1[0]) * fraction
    lat = p1[1] + (p2[1] - p1[1]) * fraction
    return [round(lon, 6), round(lat, 6)]

def point_at_distance(coords, target_km):
    """Finds [lon, lat] along coords at target_km from the start."""
    if target_km <= 0:
        return [round(coords[0][0], 6), round(coords[0][1], 6)]
    cum = 0.0
    for i in range(len(coords) - 1):
        seg = haversine_km(coords[i][0], coords[i][1], coords[i+1][0], coords[i+1][1])
        if cum + seg >= target_km:
            frac = (target_km - cum) / seg if seg > 0 else 0
            return interpolate_point(coords[i], coords[i+1], frac)
        cum += seg
    return [round(coords[-1][0], 6), round(coords[-1][1], 6)]

def closest_point_index(coords, target_lon, target_lat):
    best_i = 0
    best_dist = 1e9
    for i, pt in enumerate(coords):
        d = haversine_km(pt[0], pt[1], target_lon, target_lat)
        if d < best_dist:
            best_dist = d
            best_i = i
    return best_i, best_dist

def main():
    ensure_ne_data()
    with open(CACHE_NE, "r", encoding="utf-8") as f:
        ne = json.load(f)

    # Extract source lines
    donau_line = None
    danube_line = None
    drau_line = None
    mur_full = None

    for feat in ne.get("features", []):
        props = feat.get("properties", {})
        name = str(props.get("name") or "")
        name_en = str(props.get("name_en") or "")
        geom = feat.get("geometry", {})
        coords = geom.get("coordinates", [])

        if name == "Donau" or (name_en == "Danube" and props.get("featurecla") == "River" and name == "Donau"):
            donau_line = coords[0] if geom["type"] == "MultiLineString" else coords
        elif name == "Danube" and geom["type"] == "MultiLineString":
            danube_line = coords[0]
        elif name == "Drau" and geom["type"] == "MultiLineString" and len(coords) >= 2:
            # part 1 is main upper Drava
            drau_line = coords[1]
        elif name == "Mur" and geom["type"] == "MultiLineString":
            mur_full = coords[0]

    print(f"Extracted raw lines: Donau={len(donau_line or [])}, Danube={len(danube_line or [])}, Drau={len(drau_line or [])}, Mur={len(mur_full or [])}")

    # --- 1. MURA ---
    # In Mur, Legrad confluence is around index 188 (lat ~46.314, lon ~16.878)
    # The Mura part is from start (in Austria) to Legrad (point 188).
    # Let's verify Legrad point
    legrad_idx, _ = closest_point_index(mur_full, 16.878, 46.314)
    mura_coords = mur_full[:legrad_idx + 1]
    # In downstream order: upstream (Austria) -> downstream (Legrad, rkm 0)
    # Let's check direction: start is lat ~47.16 (Austria), end is lat ~46.31 (Legrad). Correct!
    # Chainage: Legrad is rkm 0.0. As we go upstream, rkm INCREASES.
    # Total length from Legrad to Austrian border:
    # Let's find Letenye (lon 16.69, lat 46.43, rkm 35.6) and Gornja Radgona (lon 15.99, lat 46.69, rkm 108.5)
    letenye_idx, _ = closest_point_index(mura_coords, 16.685, 46.434)
    radgona_idx, _ = closest_point_index(mura_coords, 15.988, 46.690)
    print(f"Mura: total points={len(mura_coords)}, Legrad={legrad_idx}, Letenye={letenye_idx}, Radgona={radgona_idx}")

    # Build Mura from Radgona area or whole Mura? Let's keep from around Austrian/Slovenian stretch down to Legrad
    # Index 100 is around Graz/Slovenian border
    mura_final_coords = mura_coords[100:]  # starts slightly upstream of Gornja Radgona down to Legrad

    # Now calculate cumulative distance from Legrad (rkm 0) going UPSTREAM
    # We invert coords to measure from Legrad (rkm 0) up:
    mura_rev = list(reversed(mura_final_coords)) # index 0 is Legrad (rkm 0)
    rev_len = line_length(mura_rev)
    print(f"Mura length from Legrad upstream: {rev_len:.1f} km")

    # Calibrate rkm using Letenye (rkm 35.6) and Gornja Radgona (rkm 108.5)
    # Let's place RKM markers every 10 km from 0 to 110:
    mura_rkm_features = []
    # Ratio: rev_len in km corresponds to actual rkm
    dist_letenye = line_length(mura_rev[:closest_point_index(mura_rev, 16.685, 46.434)[0] + 1])
    dist_radgona = line_length(mura_rev[:closest_point_index(mura_rev, 15.988, 46.690)[0] + 1])
    print(f"Mura rev dist to Letenye: {dist_letenye:.1f} km (ref 35.6), to Radgona: {dist_radgona:.1f} km (ref 108.5)")

    scale_mura = dist_radgona / 108.5 if dist_radgona > 0 else 1.0
    for rkm in range(0, 120, 10):
        target_dist = rkm * scale_mura
        if target_dist <= rev_len:
            pt = point_at_distance(mura_rev, target_dist)
            mura_rkm_features.append({
                "type": "Feature",
                "properties": {
                    "tip": "rkm",
                    "rkm": rkm,
                    "oznaka": f"rkm {rkm}"
                },
                "geometry": {
                    "type": "Point",
                    "coordinates": pt
                }
            })

    mura_geojson = {
        "type": "FeatureCollection",
        "name": "rijeka-mura",
        "crs": { "type": "name", "properties": { "name": "urn:ogc:def:crs:OGC:1.3:CRS84" } },
        "features": [
            {
                "type": "Feature",
                "properties": {
                    "tip": "vodotok",
                    "naziv": "Mura",
                    "sifra": "rijeka-mura",
                    "poredak": 1
                },
                "geometry": {
                    "type": "LineString",
                    "coordinates": mura_final_coords
                }
            }
        ] + mura_rkm_features
    }

    with open("data/geometrija/rijeka-mura.geojson", "w", encoding="utf-8") as f:
        json.dump(mura_geojson, f, ensure_ascii=False, indent=2)
    print("Saved data/geometrija/rijeka-mura.geojson")

    # --- 2. DRAVA ---
    # Drava = upper Drava (drau_line) from Lavamünd/Ptuj to Legrad + lower Drava (mur_full[legrad_idx:]) to Aljmaš
    # Let's inspect drau_line: starts at (12.295, 46.733) in Italy/Austria, ends at (16.878, 46.314) at Legrad!
    # mur_full[legrad_idx:] starts at (16.878, 46.314) and ends at (18.928, 45.553) at Aljmaš!
    # Connect them!
    # Let's start upper Drava from around Lavamünd (lat ~46.64, lon ~14.94) down to Legrad:
    lavamund_idx, _ = closest_point_index(drau_line, 14.940, 46.641)
    drava_upper = drau_line[lavamund_idx:]
    drava_lower = mur_full[legrad_idx:]
    drava_coords = drava_upper + drava_lower[1:] # avoid duplicate point at Legrad

    # Verify direction: Lavamünd (start) -> Ptuj -> Legrad -> Osijek -> Aljmaš (end at mouth of Drava, rkm 0)
    # Aljmaš is at the end: rkm 0.0. Going upstream, rkm INCREASES.
    drava_rev = list(reversed(drava_coords)) # index 0 is Aljmaš (rkm 0)
    drava_rev_len = line_length(drava_rev)
    print(f"Drava total length from Aljmaš upstream to Lavamünd: {drava_rev_len:.1f} km")

    # Reference stations along Drava (from Aljmaš rkm 0 upstream):
    # Osijek: rkm 19.0 (18.68, 45.56)
    # Belišće: rkm 53.0 (18.41, 45.69)
    # Drávaszabolcs / D. Miholjac: rkm 77.7 (18.17, 45.76)
    # Szentborbás: rkm 133.1 (17.65, 45.89)
    # Barcs: rkm 154.1 (17.46, 45.96)
    # Vízvár: rkm 187.6 (17.23, 46.09)
    # Legrad / Őrtilos: rkm 235.9 (16.88, 46.31)
    # Ptuj: rkm ~330 (15.87, 46.42)
    # Lavamünd: rkm ~415 (14.94, 46.64)

    idx_legrad_rev, _ = closest_point_index(drava_rev, 16.878, 46.314)
    dist_to_legrad = line_length(drava_rev[:idx_legrad_rev + 1])
    scale_drava_lower = dist_to_legrad / 235.9
    print(f"Drava dist to Legrad: {dist_to_legrad:.1f} km (ref 235.9 rkm), scale={scale_drava_lower:.3f}")

    drava_rkm_features = []
    # 0 to 240 every 10 rkm
    for rkm in range(0, 240, 10):
        target_dist = rkm * scale_drava_lower
        pt = point_at_distance(drava_rev, target_dist)
        drava_rkm_features.append({
            "type": "Feature",
            "properties": {
                "tip": "rkm",
                "rkm": rkm,
                "oznaka": f"rkm {rkm}"
            },
            "geometry": {
                "type": "Point",
                "coordinates": pt
            }
        })
    # Above Legrad (rkm 240 to 420 every 20 km)
    scale_drava_upper = (drava_rev_len - dist_to_legrad) / (415 - 235.9)
    for rkm in range(240, 421, 20):
        target_dist = dist_to_legrad + (rkm - 235.9) * scale_drava_upper
        if target_dist <= drava_rev_len:
            pt = point_at_distance(drava_rev, target_dist)
            drava_rkm_features.append({
                "type": "Feature",
                "properties": {
                    "tip": "rkm",
                    "rkm": rkm,
                    "oznaka": f"rkm {rkm}"
                },
                "geometry": {
                    "type": "Point",
                    "coordinates": pt
                }
            })

    drava_geojson = {
        "type": "FeatureCollection",
        "name": "rijeka-drava",
        "crs": { "type": "name", "properties": { "name": "urn:ogc:def:crs:OGC:1.3:CRS84" } },
        "features": [
            {
                "type": "Feature",
                "properties": {
                    "tip": "vodotok",
                    "naziv": "Drava",
                    "sifra": "rijeka-drava",
                    "poredak": 1
                },
                "geometry": {
                    "type": "LineString",
                    "coordinates": drava_coords
                }
            }
        ] + drava_rkm_features
    }

    with open("data/geometrija/rijeka-drava.geojson", "w", encoding="utf-8") as f:
        json.dump(drava_geojson, f, ensure_ascii=False, indent=2)
    print("Saved data/geometrija/rijeka-drava.geojson")

    # --- 3. DUNAV ---
    # Donau part 3 (reversed) + Danube part 0:
    # Donau part 3: (48.928, 12.509) near Regensburg -> (48.061, 17.206) Bratislava
    # Danube part 0: (48.061, 17.206) Bratislava -> past Budapest, Batina, Vukovar, Ilok, Belgrade
    donau_p3 = []
    for feat in ne.get("features", []):
        props = feat.get("properties", {})
        if props.get("name") == "Donau" and len(feat["geometry"]["coordinates"]) >= 4:
            donau_p3 = feat["geometry"]["coordinates"][3]
            break

    donau_upper = list(reversed(donau_p3))
    # Connect with danube_line
    dunav_coords = donau_upper + danube_line[1:]
    print(f"Dunav total points: {len(dunav_coords)}")

    # Cut off downstream around Belgrade (lat ~44.8, lon ~20.5)
    belgrade_idx, _ = closest_point_index(dunav_coords, 20.50, 44.82)
    dunav_coords = dunav_coords[:belgrade_idx + 1]

    idx_ilok, _ = closest_point_index(dunav_coords, 19.38, 45.22)
    idx_batina, _ = closest_point_index(dunav_coords, 18.855, 45.846)
    idx_bratislava, _ = closest_point_index(dunav_coords, 17.11, 48.14)
    print(f"Dunav: idx_bratislava={idx_bratislava}, idx_batina={idx_batina}, idx_ilok={idx_ilok}")

    # Flow is upstream -> downstream:
    # Schwabelweis (rkm ~2380) -> Passau (rkm ~2225) -> Bratislava (rkm 1872) -> Budapest (rkm 1646) -> Batina (rkm 1424.85) -> Vukovar (rkm 1333) -> Ilok (rkm 1298.8) -> Belgrade
    dist_batina_to_ilok = line_length(dunav_coords[idx_batina:idx_ilok + 1])
    delta_rkm_hr = 1424.85 - 1298.80 # 126.05 km
    scale_hr = dist_batina_to_ilok / delta_rkm_hr
    print(f"Dunav HR section: dist={dist_batina_to_ilok:.1f} km, ref delta={delta_rkm_hr:.1f} km, scale={scale_hr:.3f}")

    dunav_rkm_features = []
    # Generate RKM every 10 km through Croatian / neighboring stretch (1290 to 1450):
    for rkm in range(1290, 1451, 10):
        delta_from_batina = (1424.85 - rkm) * scale_hr
        if delta_from_batina >= 0:
            pt = point_at_distance(dunav_coords[idx_batina:], delta_from_batina)
        else:
            pt = point_at_distance(list(reversed(dunav_coords[:idx_batina+1])), -delta_from_batina)
        dunav_rkm_features.append({
            "type": "Feature",
            "properties": {
                "tip": "rkm",
                "rkm": rkm,
                "oznaka": f"rkm {rkm}"
            },
            "geometry": {
                "type": "Point",
                "coordinates": pt
            }
        })

    # Upstream markers every 50 km (1500 to 2350):
    dist_bratislava_to_batina = line_length(dunav_coords[idx_bratislava:idx_batina + 1])
    scale_upstream = dist_bratislava_to_batina / (1872.0 - 1424.85)
    for rkm in range(1500, 2400, 50):
        delta = (rkm - 1424.85) * scale_upstream
        pt = point_at_distance(list(reversed(dunav_coords[:idx_batina+1])), delta)
        dunav_rkm_features.append({
            "type": "Feature",
            "properties": {
                "tip": "rkm",
                "rkm": rkm,
                "oznaka": f"rkm {rkm}"
            },
            "geometry": {
                "type": "Point",
                "coordinates": pt
            }
        })

    dunav_geojson = {
        "type": "FeatureCollection",
        "name": "rijeka-dunav",
        "crs": { "type": "name", "properties": { "name": "urn:ogc:def:crs:OGC:1.3:CRS84" } },
        "features": [
            {
                "type": "Feature",
                "properties": {
                    "tip": "vodotok",
                    "naziv": "Dunav",
                    "sifra": "rijeka-dunav",
                    "poredak": 1
                },
                "geometry": {
                    "type": "LineString",
                    "coordinates": dunav_coords
                }
            }
        ] + dunav_rkm_features
    }

    with open("data/geometrija/rijeka-dunav.geojson", "w", encoding="utf-8") as f:
        json.dump(dunav_geojson, f, ensure_ascii=False, indent=2)
    print("Saved data/geometrija/rijeka-dunav.geojson")

if __name__ == "__main__":
    main()
