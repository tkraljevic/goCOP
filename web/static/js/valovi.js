// Stranica prijave: u plavoj plohi uz znak polako se miču valovi. Površina
// je zbroj nekoliko slojeva šuma, sjenčana kao da je osvijetljena s
// lijeva, s blagim crtama poput izohipsa. Boje se čitaju iz teme, pa
// valovi prate svijetlu i tamnu temu.
//
// Ukras ne smije smetati prijavi: crta se upola manjom razlučivošću,
// najviše 30 puta u sekundi, stane kad stranica nije vidljiva, a uz
// „smanji pokrete” u sustavu ostaje jedna mirna slika. Bez WebGL-a
// ostaje obična plava ploha.
(function () {
  var ploha = document.querySelector('.prijava-znak');
  if (!ploha) return;
  var platno = document.createElement('canvas');
  platno.className = 'prijava-valovi';
  platno.setAttribute('aria-hidden', 'true');
  var gl = platno.getContext('webgl', { alpha: false, antialias: false, depth: false, powerPreference: 'low-power' });
  if (!gl) return;
  ploha.insertBefore(platno, ploha.firstChild);

  var VERT = 'attribute vec2 p;void main(){gl_Position=vec4(p,0.,1.);}';
  var FRAG = [
    '#ifdef GL_FRAGMENT_PRECISION_HIGH\nprecision highp float;\n#else\nprecision mediump float;\n#endif',
    'uniform vec2 r;uniform float t;uniform vec3 c0,c1,c2;',
    'float h(vec2 p){return fract(sin(dot(p,vec2(127.1,311.7)))*43758.5453);}',
    'float n(vec2 p){vec2 i=floor(p),f=fract(p);f=f*f*(3.-2.*f);',
    ' return mix(mix(h(i),h(i+vec2(1,0)),f.x),mix(h(i+vec2(0,1)),h(i+vec2(1,1)),f.x),f.y);}',
    'float fbm(vec2 p){float s=0.,a=.5;mat2 m=mat2(1.6,1.2,-1.2,1.6);',
    ' for(int i=0;i<4;i++){s+=a*n(p);p=m*p;a*=.5;}return s;}',
    'float vis(vec2 p){vec2 q=vec2(fbm(p+vec2(0.,t*.02)),fbm(p+vec2(5.2,1.3)-t*.015));',
    ' return fbm(p+1.6*q+vec2(t*.01,0.));}',
    'void main(){',
    ' vec2 p=gl_FragCoord.xy/r.y*1.3;p.x*=.6;',
    ' float v=vis(p);',
    ' float e=.01;',
    ' vec3 nr=normalize(vec3(vis(p-vec2(e,0.))-vis(p+vec2(e,0.)),vis(p-vec2(0.,e))-vis(p+vec2(0.,e)),e*6.));',
    ' float svj=dot(nr,normalize(vec3(-.6,.5,.6)));',
    ' vec3 c=mix(c0,c1,smoothstep(.25,.8,v));',
    ' c+=(svj-.5)*.06;',
    ' float iz=abs(fract(v*6.)-.5);',
    ' c=mix(c,c2,(1.-smoothstep(0.,.025,iz))*.07);',
    ' vec2 u=gl_FragCoord.xy/r;c*=.88+.12*smoothstep(1.2,.2,length(u-vec2(.25,.55)));',
    ' gl_FragColor=vec4(c,1.);}'
  ].join('\n');

  function sjencar(vrsta, izvor) {
    var s = gl.createShader(vrsta);
    gl.shaderSource(s, izvor);
    gl.compileShader(s);
    return gl.getShaderParameter(s, gl.COMPILE_STATUS) ? s : null;
  }
  var vs = sjencar(gl.VERTEX_SHADER, VERT), fs = sjencar(gl.FRAGMENT_SHADER, FRAG);
  if (!vs || !fs) { platno.remove(); return; }
  var prog = gl.createProgram();
  gl.attachShader(prog, vs); gl.attachShader(prog, fs); gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) { platno.remove(); return; }
  gl.useProgram(prog);
  gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
  var ap = gl.getAttribLocation(prog, 'p');
  gl.enableVertexAttribArray(ap);
  gl.vertexAttribPointer(ap, 2, gl.FLOAT, false, 0, 0);
  var uR = gl.getUniformLocation(prog, 'r'), uT = gl.getUniformLocation(prog, 't');
  var uC = ['c0', 'c1', 'c2'].map(function (i) { return gl.getUniformLocation(prog, i); });

  // Boje: osnova je plava ploha teme, svjetliji val i crte izohipsa
  function rgb(boja) {
    var m = boja.match(/\d+(\.\d+)?/g) || [23, 62, 116];
    return [m[0] / 255, m[1] / 255, m[2] / 255];
  }
  function mijesaj(a, b, k) { return a.map(function (x, i) { return x + (b[i] - x) * k; }); }
  function boje() {
    var osnova = rgb(getComputedStyle(ploha).backgroundColor);
    gl.uniform3fv(uC[0], mijesaj(osnova, [0, 0, 0], 0.18));
    gl.uniform3fv(uC[1], mijesaj(osnova, [0.55, 0.75, 1], 0.22));
    gl.uniform3fv(uC[2], mijesaj(osnova, [1, 1, 1], 0.6));
  }

  function velicina() {
    var k = Math.min(window.devicePixelRatio || 1, 2) * 0.5;
    var w = Math.max(1, Math.round(ploha.clientWidth * k)), h = Math.max(1, Math.round(ploha.clientHeight * k));
    if (platno.width !== w || platno.height !== h) {
      platno.width = w; platno.height = h;
      gl.viewport(0, 0, w, h);
      gl.uniform2f(uR, w, h);
    }
  }

  var mirno = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)');
  var pocetak = performance.now() - 40000, zadnje = 0, okvir = 0;
  function crtaj(sad) {
    okvir = 0;
    if (document.hidden || ploha.clientWidth === 0) return;
    if (sad - zadnje >= 33) {
      zadnje = sad;
      velicina();
      gl.uniform1f(uT, (sad - pocetak) / 1000);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
    }
    if (!(mirno && mirno.matches)) okvir = requestAnimationFrame(crtaj);
  }
  function pokreni() {
    if (!okvir) okvir = requestAnimationFrame(function (s) { zadnje = 0; crtaj(s); });
  }

  boje();
  pokreni();
  document.addEventListener('visibilitychange', pokreni);
  window.addEventListener('resize', pokreni);
  if (mirno && mirno.addEventListener) mirno.addEventListener('change', pokreni);
  // Promjena teme (js/tema.js) mijenja boju plohe
  new MutationObserver(function () { boje(); zadnje = 0; pokreni(); })
    .observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
})();
