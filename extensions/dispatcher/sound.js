globalThis.DispatchWatchSound = (() => {
  let context;
  function unlock() { context ||= new AudioContext(); return context.resume(); }
  async function play(sound, volume) {
    if (sound === "silent" || !volume) return;
    let timeout;
    try {
      await Promise.race([unlock(), new Promise((_, reject) => {
        timeout = setTimeout(() => reject(new Error("Sound is suspended. Return to the tab and press Pause, then Play to enable it.")), 2000);
      })]);
    } finally { clearTimeout(timeout); }
    if (context.state !== "running") throw new Error("Sound is blocked. Press Pause, then Play on the page to enable it.");
    const notes = sound === "bell" ? [880, 1320] : sound === "radar" ? [660, 660, 990] : [523.25, 659.25, 783.99];
    notes.forEach((frequency, i) => {
      const oscillator = context.createOscillator(), gain = context.createGain(), start = context.currentTime + i * 0.17;
      oscillator.type = "sine"; oscillator.frequency.value = frequency;
      gain.gain.setValueAtTime(0, start);
      gain.gain.linearRampToValueAtTime((volume / 100) * 0.25, start + 0.015);
      gain.gain.exponentialRampToValueAtTime(0.001, start + 0.42);
      oscillator.connect(gain); gain.connect(context.destination);
      oscillator.start(start); oscillator.stop(start + 0.45);
    });
  }
  return { unlock, play };
})();
