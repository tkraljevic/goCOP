"""Ritam ispuštanja HEP-ovih elektrana na Dravi: istjecanje prema razini
akumulacije, dnevni ritam, i predvidljivost istjecanja 6–24 h unaprijed."""
import sqlite3, numpy as np, datetime, collections
db=sqlite3.connect('file:/Users/tomislavkraljevic/projekti/goCOP/data/vodostaji.db?mode=ro',uri=True)
def niz(letva, vel):
    return dict(db.execute("select vrijeme, vrijednost from spoj where letva=? and velicina=? and korak='satni'",(letva,vel)))
S={}
for l in ['he-formin','brana-he-formin','he-varazdin','brana-he-varazdin','he-cakovec','brana-he-cakovec','he-dubrava','brana-he-dubrava']:
    S[l]=niz(l,'protok')
for l in ['gvb-he-varazdin','gvb-he-cakovec','gvb-he-dubrava','he-varazdin-rep','he-dubrava-ok']:
    S[l]=niz(l,'kota')
t0=int(datetime.datetime(2017,1,1,tzinfo=datetime.timezone.utc).timestamp()); t1=int(datetime.datetime(2026,9,23,tzinfo=datetime.timezone.utc).timestamp())
T=np.arange(t0,t1,3600)
def arr(l): 
    d=S[l]; return np.array([d.get(int(t),np.nan) for t in T])
A={l:arr(l) for l in S}
print('sati',len(T))
def q(x,p): 
    x=x[~np.isnan(x)]; return np.percentile(x,p) if len(x) else np.nan
for el,gvb,br in [('he-varazdin','gvb-he-varazdin','brana-he-varazdin'),('he-cakovec','gvb-he-cakovec','brana-he-cakovec'),('he-dubrava','gvb-he-dubrava','brana-he-dubrava')]:
    Q=A[el]; K=A[gvb]/100; B=A[br]
    ok=~np.isnan(Q)&~np.isnan(K)
    print(f"\n=== {el}: sati {ok.sum()}, razina p5/p50/p95 {q(K[ok],5):.2f}/{q(K[ok],50):.2f}/{q(K[ok],95):.2f} m, istjecanje p50/p95/max {q(Q[ok],50):.0f}/{q(Q[ok],95):.0f}/{np.nanmax(Q[ok]):.0f} m3/s")
    # istjecanje po razini (razredi 10 cm)
    lo,hi=np.floor(q(K[ok],1)*10)/10, np.ceil(q(K[ok],99)*10)/10
    print("razina m | sati | Q p10 p50 p90 | preljev>0 % | sr. preljev")
    for a in np.arange(lo,hi,0.10):
        m=ok&(K>=a)&(K<a+0.10)
        if m.sum()<200: continue
        b=B[m]; b=b[~np.isnan(b)]
        print(f"{a:6.2f}–{a+0.1:5.2f} | {m.sum():6d} | {q(Q[m],10):5.0f} {q(Q[m],50):5.0f} {q(Q[m],90):5.0f} | {100*np.mean(b>5) if len(b) else float('nan'):5.1f} | {np.mean(b) if len(b) else float('nan'):6.0f}")
    # dnevni ritam: srednje istjecanje i razina po satu dana (UTC+1)
    hod=collections.defaultdict(list); hodK=collections.defaultdict(list)
    for i,t in enumerate(T):
        if ok[i]: h=(t//3600+1)%24; hod[h].append(Q[i]); hodK[h].append(K[i])
    print("sat (MEZ): sr. Q / sr. razina:", ' '.join(f"{h:02d}:{np.mean(hod[h]):.0f}/{np.mean(hodK[h]):.2f}" for h in range(0,24,3)))
    # radni dan vs vikend
    wd=collections.defaultdict(list)
    for i,t in enumerate(T):
        if ok[i]: wd[datetime.datetime.utcfromtimestamp(t).weekday()].append(Q[i])
    print("dan u tjednu sr. Q:", ' '.join(f"{d}:{np.mean(wd[d]):.0f}" for d in range(7)))
# predvidljivost istjecanja HE Dubrava i HE Varaždin: postojanost vs regresija
def znac(i, el, gvb, dotok):
    x=[1, A[el][i], A[el][i]-A[el][i-3], A[gvb][i], A[gvb][i]-A[gvb][i-3], A[gvb][i]-A[gvb][i-24], A[dotok][i], A[dotok][i]-A[dotok][i-6]]
    h=(T[i]//3600+1)%24; x+= [np.sin(2*np.pi*h/24), np.cos(2*np.pi*h/24)]
    wd=datetime.datetime.utcfromtimestamp(T[i]).weekday(); x+=[1.0 if wd>=5 else 0.0]
    return x
for el,gvb,dotok in [('he-varazdin','gvb-he-varazdin','he-formin'),('he-cakovec','gvb-he-cakovec','he-varazdin'),('he-dubrava','gvb-he-dubrava','he-cakovec')]:
    print(f"\n=== predvidljivost {el} (ulazi: {el}, {gvb}, {dotok}); uči 2017–2023, ocjena 2024–26")
    for k in (3,6,12,24,48):
        X=[];Y=[];P=[];test=[]
        for i in range(24,len(T)-k):
            x=znac(i,el,gvb,dotok); y=A[el][i+k]
            if any(np.isnan(v) for v in x) or np.isnan(y): continue
            X.append(x);Y.append(y);P.append(A[el][i]);test.append(T[i]>=int(datetime.datetime(2024,1,1,tzinfo=datetime.timezone.utc).timestamp()))
        X=np.array(X);Y=np.array(Y);P=np.array(P);test=np.array(test)
        # dva režima po razini akumulacije: ispod/iznad medijana? jednostavno: sve zajedno + kvadrat razine
        b=np.linalg.lstsq(X[~test],Y[~test],rcond=None)[0]
        Yh=X[test]@b
        # bez razine (samo Q, dotok, sat)
        idx=[0,1,2,6,7,8,9,10]; b2=np.linalg.lstsq(X[~test][:,idx],Y[~test],rcond=None)[0]; Yh2=X[test][:,idx]@b2
        e_post=np.mean(np.abs(P[test]-Y[test])); e_reg=np.mean(np.abs(Yh-Y[test])); e_bez=np.mean(np.abs(Yh2-Y[test]))
        # samo veliki dotok (val): dotok > p90
        vel=test & (X[:,6]>np.nanpercentile(X[:,6],90))
        print(f"  +{k:2d} h: postojanost {e_post:6.1f}  regresija s razinom {e_reg:6.1f}  bez razine {e_bez:6.1f} m3/s   | veliki dotok (n={vel.sum()}): post {np.mean(np.abs(P[vel]-Y[vel])):6.1f} s razinom {np.mean(np.abs((X[vel]@b)-Y[vel])):6.1f} bez {np.mean(np.abs((X[vel][:,idx]@b2)-Y[vel])):6.1f}")
