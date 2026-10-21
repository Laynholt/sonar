// "Soft retro" UI sounds: square-wave blips through a lowpass, with soft envelopes and a light echo.
type Note = [frequency: number, start: number, duration: number, volume: number, lowpass: number];

const SUCCESS: Note[] = [[987.77, 0, 0.09, 0.08, 2200], [1318.51, 0.08, 0.3, 0.08, 2200]];
const ERROR: Note[] = [[196, 0, 0.14, 0.1, 1200], [146.83, 0.14, 0.28, 0.1, 1000]];
const WARNING: Note[] = [[659.25, 0, 0.08, 0.08, 1800], [659.25, 0.12, 0.1, 0.07, 1800]];
const INFO: Note[] = [[1318.51, 0, 0.1, 0.06, 2200]];

const ATTACK = 0.003;
const SILENCE = 0.0001;

class AudioManager {
    private ctx: AudioContext | null = null;
    private bus: GainNode | null = null;

    private output(): { ctx: AudioContext; bus: GainNode } {
        if (!this.ctx || !this.bus) {
            const Ctor = window.AudioContext || (window as Window & typeof globalThis & { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
            const ctx = new Ctor();
            const bus = ctx.createGain();
            bus.gain.value = 0.9;
            const lowpass = ctx.createBiquadFilter();
            lowpass.type = "lowpass";
            lowpass.frequency.value = 7000;
            const delay = ctx.createDelay();
            delay.delayTime.value = 0.12;
            const feedback = ctx.createGain();
            feedback.gain.value = 0.2;
            const wet = ctx.createGain();
            wet.gain.value = 0.14;
            bus.connect(lowpass);
            lowpass.connect(ctx.destination);
            lowpass.connect(delay);
            delay.connect(feedback);
            feedback.connect(delay);
            delay.connect(wet);
            wet.connect(ctx.destination);
            this.ctx = ctx;
            this.bus = bus;
        }
        if (this.ctx.state === "suspended")
            void this.ctx.resume();
        return { ctx: this.ctx, bus: this.bus };
    }

    private play(notes: Note[]) {
        try {
            const { ctx, bus } = this.output();
            const now = ctx.currentTime + 0.01;
            for (const [frequency, start, duration, volume, cutoff] of notes) {
                const t = now + start;
                const osc = ctx.createOscillator();
                osc.type = "square";
                osc.frequency.value = frequency;
                const filter = ctx.createBiquadFilter();
                filter.type = "lowpass";
                filter.frequency.value = cutoff;
                const gain = ctx.createGain();
                gain.gain.setValueAtTime(SILENCE, t);
                gain.gain.exponentialRampToValueAtTime(volume, t + ATTACK);
                gain.gain.exponentialRampToValueAtTime(SILENCE, t + duration);
                osc.connect(filter);
                filter.connect(gain);
                gain.connect(bus);
                osc.start(t);
                osc.stop(t + duration + 0.05);
            }
        }
        catch (error) {
            console.error("Error playing audio:", error);
        }
    }

    playSuccess() { this.play(SUCCESS); }
    playError() { this.play(ERROR); }
    playWarning() { this.play(WARNING); }
    playInfo() { this.play(INFO); }
}

export const audioManager = new AudioManager();
export const playSuccessSound = () => audioManager.playSuccess();
export const playErrorSound = () => audioManager.playError();
export const playWarningSound = () => audioManager.playWarning();
export const playInfoSound = () => audioManager.playInfo();
