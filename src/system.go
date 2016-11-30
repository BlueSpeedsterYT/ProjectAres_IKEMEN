package main

import (
	"github.com/go-gl/gl/v2.1/gl"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/timshannon/go-openal/openal"
	"github.com/yuin/gopher-lua"
	"strings"
	"time"
)

const MaxSimul = 4

var sys = System{
	randseed:  int32(time.Now().UnixNano()),
	scrrect:   [4]int32{0, 0, 320, 240},
	gameWidth: 320, gameHeight: 240,
	widthScale: 1, heightScale: 1,
	brightness: 256,
	introTime:  0, roundTime: -1,
	lifeMul: 1, team1VS2Life: 1,
	turnsRecoveryRate: 1.0 / 300,
	zoomMin:           1, zoomMax: 1, zoomSpeed: 1,
	lifebarFontScale: 1,
	mixer:            *newMixer(),
	bgm:              *newVorbis(),
	sounds:           newSounds(),
	allPalFX:         *NewPalFX(),
	sel:              *newSelect(),
	match:            1,
	inputRemap:       [...]int{0, 1, 2, 3, 4, 5, 6, 7},
	listenPort:       "7500"}

type System struct {
	randseed                    int32
	scrrect                     [4]int32
	gameWidth, gameHeight       int32
	widthScale, heightScale     float32
	window                      *glfw.Window
	gameEnd, frameSkip          bool
	redrawWait                  struct{ nextTime, lastDraw time.Time }
	brightness                  int32
	introTime, roundTime        int32
	lifeMul, team1VS2Life       float32
	turnsRecoveryRate           float32
	zoomEnable                  bool
	zoomMin, zoomMax, zoomSpeed float32
	lifebarFontScale            float32
	debugFont                   *Fnt
	debugScript                 string
	mixer                       Mixer
	bgm                         Vorbis
	audioContext                *openal.Context
	nullSndBuf                  [audioOutLen * 2]int16
	sounds                      Sounds
	allPalFX                    PalFX
	lifebar                     Lifebar
	sel                         Select
	netInput                    *NetInput
	fileInput                   *FileInput
	aiInput                     []AiInput
	keyConfig                   []*KeyConfig
	com                         [MaxSimul * 2]int32
	autolevel                   bool
	home                        int32
	match                       int32
	inputRemap                  [MaxSimul * 2]int
	listenPort                  string
}

func (s *System) init(w, h int32) *lua.LState {
	glfw.WindowHint(glfw.Resizable, glfw.False)
	glfw.WindowHint(glfw.ContextVersionMajor, 2)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	s.setWindowSize(w, h)
	var err error
	s.window, err = glfw.CreateWindow(int(s.scrrect[2]), int(s.scrrect[3]),
		"Ikemen GO", nil, nil)
	chk(err)
	s.window.MakeContextCurrent()
	s.window.SetKeyCallback(keyCallback)
	glfw.SwapInterval(1)
	chk(gl.Init())
	s.keyConfig = append(s.keyConfig, &KeyConfig{-1,
		int(glfw.KeyUp), int(glfw.KeyDown), int(glfw.KeyLeft), int(glfw.KeyRight),
		int(glfw.KeyZ), int(glfw.KeyX), int(glfw.KeyC),
		int(glfw.KeyA), int(glfw.KeyS), int(glfw.KeyD), int(glfw.KeyEnter)})
	RenderInit()
	s.audioOpen()
	l := lua.NewState()
	l.OpenLibs()
	systemScriptInit(l)
	return l
}
func (s *System) setWindowSize(w, h int32) {
	s.scrrect[2], s.scrrect[3] = w, h
	if s.scrrect[2]*3 > s.scrrect[3]*4 {
		s.gameWidth, s.gameHeight = s.scrrect[2]*3*320/(s.scrrect[3]*4), 240
	} else {
		s.gameWidth, s.gameHeight = 320, s.scrrect[3]*4*240/(s.scrrect[2]*3)
	}
	s.widthScale = float32(s.scrrect[2]) / float32(s.gameWidth)
	s.heightScale = float32(s.scrrect[3]) / float32(s.gameHeight)
}
func (s *System) await(fps int) {
	s.playSound()
	if !s.frameSkip {
		s.window.SwapBuffers()
	}
	now := time.Now()
	diff := s.redrawWait.nextTime.Sub(now)
	wait := time.Second / time.Duration(fps)
	s.redrawWait.nextTime = s.redrawWait.nextTime.Add(wait)
	switch {
	case diff >= 0 && diff < wait+2*time.Millisecond:
		time.Sleep(diff)
		fallthrough
	case now.Sub(s.redrawWait.lastDraw) > 250*time.Millisecond:
		fallthrough
	case diff >= -17*time.Millisecond:
		s.redrawWait.lastDraw = now
		s.frameSkip = false
	default:
		if diff < -150*time.Millisecond {
			s.redrawWait.nextTime = now.Add(wait)
		}
		s.frameSkip = true
	}
	glfw.PollEvents()
	s.gameEnd = s.window.ShouldClose()
	if !s.frameSkip {
		gl.Viewport(0, 0, int32(s.scrrect[2]), int32(s.scrrect[3]))
		gl.Clear(gl.COLOR_BUFFER_BIT)
	}
}
func (s *System) resetRemapInput() {
	for i := range s.inputRemap {
		s.inputRemap[i] = i
	}
}

type SelectChar struct {
	def, name            string
	sportrait, lportrait *Sprite
}
type SelectStage struct {
	def, name string
}
type Select struct {
	columns, rows int32
	cellsize      [2]float32
	cellscale     [2]float32
	randomspr     *Sprite
	randomscl     [2]float32
	charlist      []SelectChar
	stagelist     []SelectStage
	curStageNo    int
}

func newSelect() *Select {
	return &Select{columns: 5, rows: 2, randomscl: [2]float32{1, 1},
		cellsize: [2]float32{29, 29}, cellscale: [2]float32{1, 1}}
}
func (s *Select) setStageNo(n int) int {
	s.curStageNo = n % (len(s.stagelist) + 1)
	if s.curStageNo < 0 {
		s.curStageNo += len(s.stagelist) + 1
	}
	return s.curStageNo
}
func (s *Select) AddCahr(def string) {
	s.charlist = append(s.charlist, SelectChar{})
	sc := &s.charlist[len(s.charlist)-1]
	def = strings.Replace(strings.TrimSpace(strings.Split(def, ",")[0]),
		"\\", "/", -1)
	if strings.ToLower(def) == "randomselect" {
		sc.def, sc.name = "randomselect", "Random"
		return
	}
	idx := strings.Index(def, "/")
	if len(def) >= 4 && strings.ToLower(def[len(def)-4:]) == ".def" {
		if idx < 0 {
			return
		}
	} else if idx < 0 {
		def += "/" + def + ".def"
	} else {
		def += ".def"
	}
	if def[0] != '/' || idx > 0 && strings.Index(def[:idx], ":") < 0 {
		def = "chars/" + def
	}
	if def = FileExist(def); len(def) == 0 {
		return
	}
	str, err := LoadText(def)
	if err != nil {
		return
	}
	lines, i, info, files := SplitAndTrim(str, "\n"), 0, true, true
	sprite := ""
	for i < len(lines) {
		is, name, _ := ReadIniSection(lines, &i)
		switch name {
		case "info":
			if info {
				info = false
				sc.name = is["displayname"]
				if len(sc.name) == 0 {
					sc.name = is["name"]
				}
			}
		case "files":
			if files {
				files = false
				sprite = is["sprite"]
			}
		}
	}
	sprcopy := sprite
	LoadFile(&sprite, def, func(file string) error {
		var err error
		sc.sportrait, err = LoadFromSff(file, 9000, 0)
		return err
	})
	LoadFile(&sprcopy, def, func(file string) error {
		var err error
		sc.lportrait, err = LoadFromSff(file, 9000, 1)
		return err
	})
}
func (s *Select) AddStage(def string) error {
	var lines []string
	if err := LoadFile(&def, "stages/", func(file string) error {
		str, err := LoadText(file)
		if err != nil {
			return err
		}
		lines = SplitAndTrim(str, "\n")
		return nil
	}); err != nil {
		return err
	}
	i, info := 0, false
	s.stagelist = append(s.stagelist, SelectStage{})
	ss := &s.stagelist[len(s.stagelist)-1]
	ss.def = def
	for i < len(lines) {
		is, name, _ := ReadIniSection(lines, &i)
		switch name {
		case "info":
			if info {
				info = false
				ss.name = is["displayname"]
				if len(ss.name) == 0 {
					ss.name = is["name"]
				}
			}
		}
	}
	return nil
}
