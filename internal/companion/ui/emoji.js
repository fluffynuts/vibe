// The emoji the prompt box completes: a name to type after ":", as Slack and GitHub have them (:rocket:), and what finds
// them. Pure, so it is tested apart from the page: the page draws the pop-over and puts the pick in the prompt.
//
//   find(query, limit)    the emoji whose names (or aliases) have query in them: those starting with it first,
//                         shortest first. Each is { name, char, alias } (alias: the other name it was found by, if any)
//   at(text, caret)       the ":name" being typed that ends at caret, as { start, query }, or null: it starts at the
//                         start of a word (so 12:30 and http://x aren't one), has a letter in it (or is +1 or -1), has
//                         no space in it, and isn't inside a `code span`
//   named(name)           the character a complete name stands for, or ""
//   place(measures)       where the list goes, from how the prompt measured: above the line the colon is on, its left edge
//                         at the colon and kept inside the box; below the line when there is no room above
//   emoticonBefore(text, end)  the emoticon (:) :D (: :( :/ :P ;) :'( <3) that ends at end, as { start, char }, or null: it starts a
//                         word (after a space or the start), and isn't in code
//   closing(text, caret)  the complete :name: that ends at caret, as { start, char }, or null: what typing the closing
//                         colon turns into the character
(function (root) {
  'use strict'

  // name, the character, then other names for it: one per line, as written.
  const RAW = `
smile 😄
grinning 😀
smiley 😃
grin 😁
laughing 😆 satisfied
sweat_smile 😅
rofl 🤣
joy 😂
slightly_smiling_face 🙂 slight_smile
upside_down_face 🙃
wink 😉
blush 😊
innocent 😇
smiling_face_with_three_hearts 🥰
heart_eyes 😍
star_struck 🤩
kissing_heart 😘
kissing 😗
relaxed ☺️
kissing_closed_eyes 😚
kissing_smiling_eyes 😙
yum 😋
stuck_out_tongue 😛
stuck_out_tongue_winking_eye 😜
zany_face 🤪
stuck_out_tongue_closed_eyes 😝
money_mouth_face 🤑
hugs 🤗
hand_over_mouth 🤭
shushing_face 🤫
thinking 🤔 thinking_face
zipper_mouth_face 🤐
raised_eyebrow 🤨
neutral_face 😐
expressionless 😑
no_mouth 😶
smirk 😏
unamused 😒
roll_eyes 🙄 face_with_rolling_eyes
grimacing 😬
lying_face 🤥
relieved 😌
pensive 😔
sleepy 😪
drooling_face 🤤
sleeping 😴
mask 😷
face_with_thermometer 🤒
face_with_head_bandage 🤕
nauseated_face 🤢
vomiting_face 🤮
sneezing_face 🤧
hot_face 🥵
cold_face 🥶
woozy_face 🥴
dizzy_face 😵
exploding_head 🤯
cowboy_hat_face 🤠
partying_face 🥳
sunglasses 😎
nerd_face 🤓
monocle_face 🧐
confused 😕
worried 😟
slightly_frowning_face 🙁
frowning_face ☹️
open_mouth 😮
hushed 😯
astonished 😲
flushed 😳
pleading_face 🥺
frowning 😦
anguished 😧
fearful 😨
cold_sweat 😰
disappointed_relieved 😥
cry 😢
sob 😭
scream 😱
confounded 😖
persevere 😣
disappointed 😞
sweat 😓
weary 😩
tired_face 😫
yawning_face 🥱
triumph 😤
rage 😡 pout
angry 😠
cursing_face 🤬
smiling_imp 😈
imp 👿
skull 💀
skull_and_crossbones ☠️
poop 💩 hankey shit
clown_face 🤡
japanese_ogre 👹
ghost 👻
alien 👽
robot 🤖
smiley_cat 😺
heart_eyes_cat 😻
see_no_evil 🙈
hear_no_evil 🙉
speak_no_evil 🙊
kiss 💋
love_letter 💌
cupid 💘
gift_heart 💝
sparkling_heart 💖
heartpulse 💗
heartbeat 💓
revolving_hearts 💞
two_hearts 💕
heart_decoration 💟
heavy_heart_exclamation ❣️
broken_heart 💔
heart ❤️ red_heart
orange_heart 🧡
yellow_heart 💛
green_heart 💚
blue_heart 💙
purple_heart 💜
brown_heart 🤎
black_heart 🖤
white_heart 🤍
100 💯
anger 💢
boom 💥 collision
dizzy 💫
sweat_drops 💦
dash 💨
hole 🕳️
bomb 💣
speech_balloon 💬
thought_balloon 💭
zzz 💤
wave 👋
raised_back_of_hand 🤚
raised_hand_with_fingers_splayed 🖐️ hand_splayed
raised_hand ✋ hand
vulcan_salute 🖖
ok_hand 👌
pinching_hand 🤏
v ✌️ victory_hand
crossed_fingers 🤞 fingers_crossed
love_you_gesture 🤟
metal 🤘
call_me_hand 🤙
point_left 👈
point_right 👉
point_up_2 👆
middle_finger 🖕
point_down 👇
point_up ☝️
thumbsup 👍 +1 thumbs_up
thumbsdown 👎 -1 thumbs_down
fist ✊ fist_raised
facepunch 👊 punch fist_oncoming
fist_left 🤛
fist_right 🤜
clap 👏
raised_hands 🙌
open_hands 👐
palms_up_together 🤲
handshake 🤝
pray 🙏
writing_hand ✍️
nail_care 💅
selfie 🤳
muscle 💪
mechanical_arm 🦾
leg 🦵
foot 🦶
ear 👂
nose 👃
brain 🧠
eyes 👀
eye 👁️
tongue 👅
lips 👄
baby 👶
child 🧒
boy 👦
girl 👧
adult 🧑
man 👨
woman 👩
older_man 👴
older_woman 👵
person_frowning 🙍
person_raising_hand 🙋 raising_hand
bow 🙇
facepalm 🤦 face_palm
shrug 🤷
cop 👮 police_officer
detective 🕵️
guardsman 💂
construction_worker 👷
prince 🤴
princess 👸
santa 🎅
mage 🧙
fairy 🧚
vampire 🧛
zombie 🧟
genie 🧞
merperson 🧜
elf 🧝
superhero 🦸
supervillain 🦹
ninja 🥷
dancer 💃
man_dancing 🕺
runner 🏃 running
walking 🚶
people_holding_hands 🧑‍🤝‍🧑
couple 👫
family 👪
footprints 👣
dog 🐶
cat 🐱
mouse 🐭
hamster 🐹
rabbit 🐰
fox_face 🦊 fox
bear 🐻
panda_face 🐼 panda
koala 🐨
tiger 🐯
lion 🦁
cow 🐮
pig 🐷
frog 🐸
monkey_face 🐵
monkey 🐒
chicken 🐔
penguin 🐧
bird 🐦
baby_chick 🐤
hatching_chick 🐣
eagle 🦅
owl 🦉
bat 🦇
wolf 🐺
boar 🐗
horse 🐴
unicorn 🦄
bee 🐝 honeybee
bug 🐛
butterfly 🦋
snail 🐌
ladybug 🐞 lady_beetle
ant 🐜
spider 🕷️
scorpion 🦂
turtle 🐢
snake 🐍
lizard 🦎
t_rex 🦖
sauropod 🦕
octopus 🐙
squid 🦑
shrimp 🦐
crab 🦀
fish 🐟
tropical_fish 🐠
blowfish 🐡
shark 🦈
whale 🐳
whale2 🐋
dolphin 🐬 flipper
crocodile 🐊
elephant 🐘
camel 🐪
giraffe 🦒
kangaroo 🦘
sheep 🐑
goat 🐐
deer 🦌
dragon 🐉
dragon_face 🐲
cactus 🌵
christmas_tree 🎄
evergreen_tree 🌲
deciduous_tree 🌳
palm_tree 🌴
seedling 🌱
herb 🌿
shamrock ☘️
four_leaf_clover 🍀
bamboo 🎍
maple_leaf 🍁
fallen_leaf 🍂
leaves 🍃
mushroom 🍄
sunflower 🌻
rose 🌹
tulip 🌷
cherry_blossom 🌸
hibiscus 🌺
bouquet 💐
earth_africa 🌍
earth_americas 🌎
earth_asia 🌏
globe_with_meridians 🌐
full_moon 🌕
new_moon 🌑
crescent_moon 🌙
sun_with_face 🌞
star ⭐
star2 🌟 glowing_star
sparkles ✨
zap ⚡ lightning
comet ☄️
fire 🔥 flame
tornado 🌪️
rainbow 🌈
sunny ☀️
partly_sunny ⛅
cloud ☁️
snowflake ❄️
snowman ⛄
umbrella ☂️
droplet 💧
ocean 🌊
green_apple 🍏
apple 🍎
pear 🍐
tangerine 🍊 orange mandarin
lemon 🍋
banana 🍌
watermelon 🍉
grapes 🍇
strawberry 🍓
melon 🍈
cherries 🍒
peach 🍑
mango 🥭
pineapple 🍍
coconut 🥥
kiwi_fruit 🥝
tomato 🍅
eggplant 🍆
avocado 🥑
carrot 🥕
corn 🌽
hot_pepper 🌶️
cucumber 🥒
broccoli 🥦
garlic 🧄
onion 🧅
potato 🥔
sweet_potato 🍠
croissant 🥐
bread 🍞
baguette_bread 🥖
pretzel 🥨
cheese 🧀
egg 🥚
fried_egg 🍳 cooking
bacon 🥓
pancakes 🥞
waffle 🧇
poultry_leg 🍗
meat_on_bone 🍖
hotdog 🌭
hamburger 🍔
fries 🍟
pizza 🍕
sandwich 🥪
taco 🌮
burrito 🌯
salad 🥗
spaghetti 🍝
ramen 🍜
stew 🍲
curry 🍛
sushi 🍣
bento 🍱
dumpling 🥟
rice 🍚
cookie 🍪
birthday 🎂
cake 🍰
cupcake 🧁
pie 🥧
chocolate_bar 🍫
candy 🍬
lollipop 🍭
doughnut 🍩
popcorn 🍿
icecream 🍦
ice_cream 🍨
shaved_ice 🍧
honey_pot 🍯
milk_glass 🥛
coffee ☕
tea 🍵
beer 🍺
beers 🍻
clinking_glasses 🥂
wine_glass 🍷
tumbler_glass 🥃
cocktail 🍸
tropical_drink 🍹
champagne 🍾
fork_and_knife 🍴
spoon 🥄
soccer ⚽
basketball 🏀
football 🏈
baseball ⚾
tennis 🎾
volleyball 🏐
rugby_football 🏉
8ball 🎱
golf ⛳
bowling 🎳
cricket_game 🏏
hockey 🏒
ping_pong 🏓
badminton 🏸
boxing_glove 🥊
dart 🎯
video_game 🎮
joystick 🕹️
game_die 🎲
chess_pawn ♟️
jigsaw 🧩
slot_machine 🎰
trophy 🏆
medal_sports 🏅
first_place_medal 🥇
second_place_medal 🥈
third_place_medal 🥉
art 🎨
performing_arts 🎭
musical_note 🎵
notes 🎶
microphone 🎤
headphones 🎧
guitar 🎸
violin 🎻
drum 🥁
trumpet 🎺
clapper 🎬
ticket 🎫
tada 🎉 party_popper
confetti_ball 🎊
balloon 🎈
gift 🎁
ribbon 🎀
car 🚗 red_car
taxi 🚕
bus 🚌
racing_car 🏎️
police_car 🚓
ambulance 🚑
fire_engine 🚒
truck 🚚
tractor 🚜
bike 🚲
motor_scooter 🛵
motorcycle 🏍️
train 🚆
bullettrain_side 🚄
airplane ✈️
small_airplane 🛩️
helicopter 🚁
rocket 🚀
flying_saucer 🛸
ship 🚢
sailboat ⛵
anchor ⚓
fuelpump ⛽
construction 🚧
rotating_light 🚨 siren
vertical_traffic_light 🚦
house 🏠
office 🏢
hospital 🏥
bank 🏦
school 🏫
castle 🏰
tent ⛺
mountain ⛰️
volcano 🌋
beach_umbrella 🏖️
desert_island 🏝️
camping 🏕️
watch ⌚
iphone 📱
computer 💻 laptop
desktop_computer 🖥️
keyboard ⌨️
computer_mouse 🖱️
printer 🖨️
floppy_disk 💾
cd 💿
dvd 📀
camera 📷
video_camera 📹
tv 📺
radio 📻
telephone_receiver 📞
battery 🔋
electric_plug 🔌
bulb 💡 light_bulb
flashlight 🔦
candle 🕯️
money_with_wings 💸
dollar 💵
moneybag 💰
credit_card 💳
gem 💎
scales ⚖️
wrench 🔧
hammer 🔨
hammer_and_wrench 🛠️
pick ⛏️
nut_and_bolt 🔩
gear ⚙️
chains ⛓️
toolbox 🧰
magnet 🧲
gun 🔫
knife 🔪
dagger 🗡️
shield 🛡️
microscope 🔬
telescope 🔭
satellite 📡
syringe 💉
pill 💊
door 🚪
bed 🛏️
toilet 🚽
shower 🚿
bathtub 🛁
lock 🔒
unlock 🔓
key 🔑
old_key 🗝️
bell 🔔
no_bell 🔕
bookmark 🔖
link 🔗
paperclip 📎
scissors ✂️
pushpin 📌
round_pushpin 📍
triangular_ruler 📐
straight_ruler 📏
memo 📝 pencil
pencil2 ✏️
pen 🖊️
paintbrush 🖌️
crayon 🖍️
books 📚
book 📖
notebook 📓
page_facing_up 📄
newspaper 📰
bookmark_tabs 📑
clipboard 📋
file_folder 📁
open_file_folder 📂
card_index_dividers 🗂️
calendar 📆
date 📅
chart_with_upwards_trend 📈
chart_with_downwards_trend 📉
bar_chart 📊
package 📦
mailbox 📫
email 📧 e-mail
envelope ✉️
inbox_tray 📥
outbox_tray 📤
wastebasket 🗑️
mag 🔍
mag_right 🔎
hourglass ⌛
hourglass_flowing_sand ⏳
alarm_clock ⏰
stopwatch ⏱️
timer_clock ⏲️
clock1 🕐
white_check_mark ✅
heavy_check_mark ✔️
ballot_box_with_check ☑️
x ❌
negative_squared_cross_mark ❎
heavy_multiplication_x ✖️
heavy_plus_sign ➕
heavy_minus_sign ➖
question ❓
grey_question ❔
exclamation ❗ heavy_exclamation_mark
grey_exclamation ❕
bangbang ‼️
interrobang ⁉️
warning ⚠️
no_entry ⛔
no_entry_sign 🚫
stop_sign 🛑
radioactive ☢️
biohazard ☣️
recycle ♻️
infinity ♾️
arrow_up ⬆️
arrow_down ⬇️
arrow_left ⬅️
arrow_right ➡️
arrow_upper_right ↗️
arrows_counterclockwise 🔄
arrows_clockwise 🔃
repeat 🔁
repeat_one 🔂
twisted_rightwards_arrows 🔀
rewind ⏪
fast_forward ⏩
play_or_pause_button ⏯️
arrow_forward ▶️
pause_button ⏸️
stop_button ⏹️
red_circle 🔴
large_orange_circle 🟠
large_yellow_circle 🟡
large_green_circle 🟢
large_blue_circle 🔵
large_purple_circle 🟣
black_circle ⚫
white_circle ⚪
red_square 🟥
green_square 🟩
blue_square 🟦
checkered_flag 🏁
triangular_flag_on_post 🚩
white_flag 🏳️
black_flag 🏴
rainbow_flag 🏳️‍🌈
pirate_flag 🏴‍☠️
flag-za 🇿🇦
flag-us 🇺🇸
flag-gb 🇬🇧
flag-eu 🇪🇺
flag-de 🇩🇪
flag-fr 🇫🇷
flag-au 🇦🇺
flag-ca 🇨🇦
flag-nz 🇳🇿
flag-jp 🇯🇵
flag-in 🇮🇳
flag-br 🇧🇷
flag-nl 🇳🇱
flag-ie 🇮🇪
copyright ©️
registered ®️
tm ™️
hash #️⃣
information_source ℹ️
new 🆕
free 🆓
cool 🆒
ok 🆗
sos 🆘
up 🆙
top 🔝
soon 🔜
back 🔙
end 🔚
trident 🔱
beginner 🔰
sparkle ❇️
eight_spoked_asterisk ✳️
yin_yang ☯️
peace_symbol ☮️
atom_symbol ⚛️
om 🕉️
wheel_of_dharma ☸️
star_of_david ✡️
cross ✝️
hotsprings ♨️
spades ♠️
hearts ♥️
diamonds ♦️
clubs ♣️
joker 🃏
mahjong 🀄
speaking_head 🗣️
bust_in_silhouette 👤
busts_in_silhouette 👥
crown 👑
tophat 🎩
mortar_board 🎓
lipstick 💄
ring 💍
glasses 👓
dark_sunglasses 🕶️
briefcase 💼
handbag 👜
school_satchel 🎒
shoe 👞
sneaker 👟
boot 👢
high_heel 👠
tshirt 👕
jeans 👖
dress 👗
kimono 👘
bikini 👙
umbrella_with_rain_drops ☔
snow_capped_mountain 🏔️
motorway 🛣️
world_map 🗺️
compass 🧭
moyai 🗿
statue_of_liberty 🗽
tokyo_tower 🗼
ferris_wheel 🎡
roller_coaster 🎢
carousel_horse 🎠
fireworks 🎆
sparkler 🎇
sunrise 🌅
city_sunset 🌇
night_with_stars 🌃
milky_way 🌌
bridge_at_night 🌉
`

  const entries = []
  const byName = new Map()
  for (const line of RAW.split('\n')) {
    const parts = line.trim().split(' ')
    if (parts.length < 2) continue
    const [name, char, ...aliases] = parts
    if (byName.has(name)) continue
    const entry = { name, char, aliases }
    entries.push(entry)
    byName.set(name, entry)
    for (const alias of aliases) if (!byName.has(alias)) byName.set(alias, entry)
  }

  // The emoticons that become an emoji when the word is finished. Few, on purpose: each is a way for ordinary text to change.
  // Longest first, so that :'( is taken whole. The letters match in either case.
  const EMOTICONS = [
    { text: ":'(", name: 'cry', char: '😢' },
    { text: ':D', name: 'grinning', char: '😀' },
    { text: ':)', name: 'slightly_smiling_face', char: '🙂' },
    { text: '(:', name: 'slightly_smiling_face', char: '🙂' },
    { text: ':(', name: 'slightly_frowning_face', char: '🙁' },
    { text: ':/', name: 'confused', char: '😕' },
    { text: ':P', name: 'stuck_out_tongue', char: '😛' },
    { text: ':O', name: 'open_mouth', char: '😮' },
    { text: ';)', name: 'wink', char: '😉' },
    { text: '<3', name: 'heart', char: '❤️' },
  ]
  // The ones that are also the start of a name typed after a colon (:D, :P, :O): offered first while it is typed.
  const LEAD = new Map(EMOTICONS.filter((e) => /^:[A-Z]$/.test(e.text)).map((e) => [e.text[1].toLowerCase(), e]))

  // emoticonBefore is the emoticon that ends at end of text: one that starts a word (after a space, a new line or the start:
  // not in http:/ or C:/ or 12:( ), and is not in code.
  function emoticonBefore(text, end) {
    for (const e of EMOTICONS) {
      const start = end - e.text.length
      if (start < 0 || text.slice(start, end).toLowerCase() !== e.text.toLowerCase()) continue
      if (start > 0 && !/\s/.test(text[start - 1])) continue
      if (insideCode(text, start)) continue
      return { start, char: e.char }
    }
    return null
  }

  // find is the emoji with query in a name or an alias: those starting with it first, shortest first, so the closest
  // match leads; one found only by an alias says which.
  function find(query, limit) {
    const q = String(query || '').toLowerCase()
    if (q.length === 0) return []
    const starts = []
    const within = []
    for (const e of entries) {
      const names = [e.name].concat(e.aliases)
      let best = -1
      let by = ''
      for (const n of names) {
        const at = n.indexOf(q)
        if (at < 0) continue
        if (best < 0 || at < best || (at === best && n.length < by.length)) { best = at; by = n }
      }
      if (best < 0) continue
      const hit = { name: e.name, char: e.char, alias: by === e.name ? '' : by, matched: by }
      ;(best === 0 ? starts : within).push(hit)
    }
    const order = (a, b) => a.matched.length - b.matched.length || a.name.localeCompare(b.name)
    starts.sort(order)
    within.sort(order)
    let all = starts.concat(within).map(({ name, char, alias }) => ({ name, char, alias }))
    // :D is 😀 and not whatever else has a d in it: the emoticon leads, and says so.
    const lead = LEAD.get(q)
    if (lead) all = [{ name: lead.name, char: lead.char, alias: '', emoticon: lead.text }].concat(all.filter((e) => e.name !== lead.name))
    return typeof limit === 'number' ? all.slice(0, limit) : all
  }

  const NAME_CHARS = /[a-z0-9_+-]/i

  // insideCode says position pos of text is in code: in a fence that has been opened and not closed (which may run across
  // blank lines), or in a `code span` that has: an odd number of backticks since the last blank line. A span doesn't cross a
  // blank line, so one stray backtick costs the paragraph it is in, not the rest of the message.
  function insideCode(text, pos) {
    const before = text.slice(0, pos)
    const fences = before.match(/^ {0,3}(?:`{3,}|~{3,})/gm)
    if (fences && fences.length % 2 === 1) return true
    const blank = [...before.matchAll(/\n[ \t]*\n/g)].pop()
    const paragraph = blank ? before.slice(blank.index + blank[0].length) : before
    const ticks = paragraph.replace(/^ {0,3}(?:`{3,}|~{3,}).*$/gm, '').split('`').length - 1
    return ticks % 2 === 1
  }

  // startOfWord says a colon at i opens a name: at the start of the text, or after a space or an opening bracket or quote.
  function startOfWord(text, i) {
    return i === 0 || /[\s([{"'<]/.test(text[i - 1])
  }

  function at(text, caret) {
    let i = caret
    while (i > 0 && NAME_CHARS.test(text[i - 1])) i--
    if (i === 0 || text[i - 1] !== ':') return null
    const start = i - 1
    const query = text.slice(i, caret)
    // One character is enough, and Tab is what picks, so ":D" is only ever written. It has a letter in it, or is a sign and
    // digits (+1, -1): not digits alone, nor signs alone.
    if (query.length < 1 || !(/[a-z]/i.test(query) || /^[+-]\d*$/.test(query))) return null
    if (!startOfWord(text, start) || insideCode(text, start)) return null
    return { start, query }
  }

  function named(name) {
    const e = byName.get(String(name || '').toLowerCase())
    return e ? e.char : ''
  }

  function closing(text, caret) {
    if (caret < 3 || text[caret - 1] !== ':') return null
    let i = caret - 1
    while (i > 0 && NAME_CHARS.test(text[i - 1])) i--
    if (i === 0 || text[i - 1] !== ':') return null
    const start = i - 1
    const char = named(text.slice(i, caret - 1))
    if (!char || !startOfWord(text, start) || insideCode(text, start)) return null
    return { start, char }
  }

  // place is where the list goes, in the box's own coordinates: { left, top, below }. caretLeft, caretTop and caretHeight are
  // the colon's line (from the box's top left); roomAbove is the pixels from the top of the page to that line; the rest are
  // sizes. Anything not measured (no layout) is taken as zero, so the list is never put at NaN.
  function place(m) {
    const n = (v) => (Number.isFinite(v) ? v : 0)
    const gap = 4
    const listWidth = n(m.listWidth)
    const listHeight = n(m.listHeight)
    const caretTop = n(m.caretTop)
    const left = Math.max(0, Math.min(n(m.caretLeft), n(m.boxWidth) - listWidth))
    const above = n(m.roomAbove) >= listHeight + gap
    return above
      ? { left, top: caretTop - listHeight - gap, below: false }
      : { left, top: caretTop + n(m.caretHeight) + gap, below: true }
  }

  const api = { find, at, named, closing, place, emoticonBefore, count: entries.length }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeEmoji = api
})(this)
