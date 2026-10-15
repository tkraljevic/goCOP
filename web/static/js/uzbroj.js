// uzBroj bira oblik riječi uz broj po istom pravilu kao models.UzBrojHR u
// programu: 1 letva, 2–4 letve, 5 letvi, ali 11–14 kao 5, a 21 kao 1. Oblik
// može nositi i glagol koji se slaže s brojem. Učitava se prije ostalih
// skripti, pa ga mogu zvati i skripte napisane u stranici.
function uzBroj(n, jedan, dva, pet) {
  var z = Math.abs(n) % 10, zz = Math.abs(n) % 100;
  if (zz >= 11 && zz <= 14) { return pet; }
  if (z === 1) { return jedan; }
  if (z >= 2 && z <= 4) { return dva; }
  return pet;
}
