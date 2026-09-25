#!/usr/bin/env python3
"""Izdvoji kilometarska sidra iz lokalnog službenog Inland ENC ZIP-a.

Zahtijeva pyogrio u projektnoj .venv; ne mijenja bazu ni geometriju rijeke.
Uzima samo kategoriju catdis=1, ne miješa je s kategorijom catdis=3.
Čuva cijele kilometre i krajnje oznake, bez popravljanja anomalija izvora.
"""
import argparse
import hashlib
import json
import math
import pathlib
import struct
import zipfile

from pyogrio.raw import read


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("river", choices=["drava", "dunav"])
    p.add_argument("zip", type=pathlib.Path)
    p.add_argument("output", type=pathlib.Path)
    args = p.parse_args()
    prefix = "HRXXX00002" if args.river == "drava" else "HRXXX00001"
    bounds = (0, 24) if args.river == "drava" else (1294, 1434)
    points, rejected = {}, []
    with zipfile.ZipFile(args.zip) as archive:
        for cell in sorted(archive.namelist()):
            if not cell.endswith(".000") or cell.endswith("CATALOG.000"):
                continue
            meta, _, geometries, arrays = read(
                f"/vsizip/{args.zip.resolve()}/{cell}", layer="dismar",
                columns=["catdis", "wtwdis", "unlocd"])
            cols = dict(zip(meta["fields"], arrays))
            if meta["crs"] != "EPSG:4326":
                raise ValueError(f"očekivan WGS84, dobiven {meta['crs']}")
            for i, value in enumerate(cols["wtwdis"]):
                if cols["catdis"][i] != 1 or not str(cols["unlocd"][i]).startswith(prefix):
                    continue
                rkm = float(value)
                if not math.isfinite(rkm) or not bounds[0] <= rkm <= bounds[1]:
                    rejected.append({"cell": cell, "rkm": str(rkm), "id": cols["unlocd"][i]})
                    continue
                # Hrvatske ćelije kodiraju hektometar u završnih šest znamenki.
                # Ako se dva polja izvora ne slažu, zapis se odbija, ne popravlja.
                if int(str(cols["unlocd"][i])[-6:]) != round(rkm * 10):
                    rejected.append({"cell": cell, "rkm": str(rkm), "id": cols["unlocd"][i]})
                    continue
                wkb = geometries[i]
                if wkb[:5] != b"\x01\x01\x00\x00\x00" or len(wkb) != 21:
                    raise ValueError("očekivan 2D WKB Point u WGS84")
                xy = list(struct.unpack("<dd", wkb[5:]))
                if rkm in points and points[rkm]["tocka"] != xy:
                    raise ValueError(f"proturječna sidra za {rkm}; potreban ručni pregled")
                points[rkm] = {"rkm": rkm, "tocka": xy, "izvor": cell}
    if len(points) < 2:
        raise ValueError("nema dovoljno sidara")
    extremes = (min(points), max(points))
    anchors = [points[r] for r in sorted(points)
               if r in extremes or abs(r - round(r)) < 1e-6]
    result = {
        "izvor": f"https://www.vodniputovi.hr/enc/{args.river}/{args.river}.zip",
        "sha256": hashlib.sha256(args.zip.read_bytes()).hexdigest(),
        "izdanje": "2018-11-29" if args.river == "drava" else "2018-09-11",
        "napomena": "ENC dismar/catdis=1/wtwdis; informativna kalibracija OSM-a, nije za navigaciju.",
        "odbijeno": rejected, "sidra": anchors,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"{args.river}: {len(anchors)} sidara, rkm {extremes}, odbijeno {len(rejected)}")


if __name__ == "__main__":
    main()
