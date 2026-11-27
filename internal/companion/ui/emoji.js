// The emoji the prompt box completes: a name to type after ":", as Slack and GitHub have them (:rocket:), and what finds
// them. Pure, so it is tested apart from the page: the page draws the pop-over and puts the pick in the prompt.
//
//   find(query, limit)    the emoji whose names (or aliases) have query in them: those starting with it first,
//                         shortest first. Each is { name, char, alias } (alias: the other name it was found by, if any)
//   at(text, caret)       the ":name" being typed that ends at caret, as { start, query }, or null: it starts at the
//                         start of a word (so 12:30 and http://x aren't one), has a letter in it (or is +1 or -1), has
//                         no space in it, and isn't inside a `code span`
//   named(name)           the character a complete name stands for, or ""
//   drawn(text)           text with each :name: that stands apart (not stuck to a letter or digit either side: a:b:c stays) and
//                         names an emoji drawn as it, as GitHub draws them; the rest as it is
//   usual(name)           whether name is one of the usual emoji (hand-picked), which complete before the rarer ones
//   place(measures)       where the list goes, from how the prompt measured: above the line the colon is on, its left edge
//                         at the colon and kept inside the box; below the line when there is no room above
//   emoticonBefore(text, end)  the emoticon (:) :D (: :( :/ :P ;) :'( <3) that ends at end, as { start, char }, or null: it starts a
//                         word (after a space or the start), and isn't in code
//   closing(text, caret)  the complete :name: that ends at caret, as { start, char }, or null: what typing the closing
//                         colon turns into the character
(function (root) {
  'use strict'

  // name, the character, then other names for it: one per line, as written. The first lines were chosen by hand (the order ties
  // are listed in); the rest are every emoji GitHub names, with the names it gives them, from github/gemoji's db/emoji.json
  // (2026-11-27), merged in where the hand-picked lines lacked the character or a name. Its notice:
  //
  //   Copyright (c) 2019 GitHub, Inc.
  //
  //   Permission is hereby granted, free of charge, to any person
  //   obtaining a copy of this software and associated documentation
  //   files (the "Software"), to deal in the Software without
  //   restriction, including without limitation the rights to use,
  //   copy, modify, merge, publish, distribute, sublicense, and/or sell
  //   copies of the Software, and to permit persons to whom the
  //   Software is furnished to do so, subject to the following
  //   conditions:
  //
  //   The above copyright notice and this permission notice shall be
  //   included in all copies or substantial portions of the Software.
  //
  //   THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
  //   EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
  //   OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
  //   NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
  //   HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
  //   WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
  //   FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
  //   OTHER DEALINGS IN THE SOFTWARE.
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
middle_finger 🖕 fu
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
person_frowning 🙍 frowning_person
person_raising_hand 🙋 raising_hand
bow 🙇
facepalm 🤦 face_palm
shrug 🤷
cop 👮 police_officer
detective 🕵️
guardsman 💂 guard
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
dancer 💃 woman_dancing
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
t_rex 🦖 t-rex
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
camel 🐪 dromedary_camel
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
umbrella ☂️ open_umbrella
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
salad 🥗 green_salad
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
hockey 🏒 ice_hockey
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
first_place_medal 🥇 1st_place_medal
second_place_medal 🥈 2nd_place_medal
third_place_medal 🥉 3rd_place_medal
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
train 🚆 train2
bullettrain_side 🚄
airplane ✈️
small_airplane 🛩️
helicopter 🚁
rocket 🚀
flying_saucer 🛸
ship 🚢
sailboat ⛵ boat
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
castle 🏰 european_castle
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
scales ⚖️ balance_scale
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
knife 🔪 hocho
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
book 📖 open_book
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
large_orange_circle 🟠 orange_circle
large_yellow_circle 🟡 yellow_circle
large_green_circle 🟢 green_circle
large_blue_circle 🔵
large_purple_circle 🟣 purple_circle
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
flag-za 🇿🇦 south_africa
flag-us 🇺🇸 us
flag-gb 🇬🇧 gb uk
flag-eu 🇪🇺 eu european_union
flag-de 🇩🇪 de
flag-fr 🇫🇷 fr
flag-au 🇦🇺 australia
flag-ca 🇨🇦 canada
flag-nz 🇳🇿 new_zealand
flag-jp 🇯🇵 jp
flag-in 🇮🇳 india
flag-br 🇧🇷 brazil
flag-nl 🇳🇱 netherlands
flag-ie 🇮🇪 ireland
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
cross ✝️ latin_cross
hotsprings ♨️
spades ♠️
hearts ♥️
diamonds ♦️
clubs ♣️
joker 🃏 black_joker
mahjong 🀄
speaking_head 🗣️
bust_in_silhouette 👤
busts_in_silhouette 👥
crown 👑
tophat 🎩
mortar_board 🎓
lipstick 💄
ring 💍
glasses 👓 eyeglasses
dark_sunglasses 🕶️
briefcase 💼
handbag 👜
school_satchel 🎒
shoe 👞 mans_shoe
sneaker 👟 athletic_shoe
boot 👢
high_heel 👠
tshirt 👕 shirt
jeans 👖
dress 👗
kimono 👘
bikini 👙
umbrella_with_rain_drops ☔
snow_capped_mountain 🏔️ mountain_snow
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
city_sunset 🌇 city_sunrise
night_with_stars 🌃
milky_way 🌌
bridge_at_night 🌉
`

  // The rest GitHub names, as RAW: listed after RAW's when both start with what was typed, so the usual ones come first.
  const MORE = `
melting_face 🫠
smiling_face_with_tear 🥲
face_with_open_eyes_and_hand_over_mouth 🫢
face_with_peeking_eye 🫣
saluting_face 🫡
dotted_line_face 🫥
face_in_clouds 😶‍🌫️
face_exhaling 😮‍💨
shaking_face 🫨
face_with_spiral_eyes 😵‍💫
disguised_face 🥸
face_with_diagonal_mouth 🫤
face_holding_back_tears 🥹
japanese_goblin 👺
space_invader 👾
smile_cat 😸
joy_cat 😹
smirk_cat 😼
kissing_cat 😽
scream_cat 🙀
crying_cat_face 😿
pouting_cat 😾
heart_on_fire ❤️‍🔥
mending_heart ❤️‍🩹
pink_heart 🩷
light_blue_heart 🩵
grey_heart 🩶
eye_speech_bubble 👁️‍🗨️
left_speech_bubble 🗨️
right_anger_bubble 🗯️
rightwards_hand 🫱
leftwards_hand 🫲
palm_down_hand 🫳
palm_up_hand 🫴
leftwards_pushing_hand 🫷
rightwards_pushing_hand 🫸
pinched_fingers 🤌
hand_with_index_finger_and_thumb_crossed 🫰
index_pointing_at_the_viewer 🫵
heart_hands 🫶
mechanical_leg 🦿
ear_with_hearing_aid 🦻
anatomical_heart 🫀
lungs 🫁
tooth 🦷
bone 🦴
biting_lip 🫦
blond_haired_person 👱
bearded_person 🧔
man_beard 🧔‍♂️
woman_beard 🧔‍♀️
red_haired_man 👨‍🦰
curly_haired_man 👨‍🦱
white_haired_man 👨‍🦳
bald_man 👨‍🦲
red_haired_woman 👩‍🦰
person_red_hair 🧑‍🦰
curly_haired_woman 👩‍🦱
person_curly_hair 🧑‍🦱
white_haired_woman 👩‍🦳
person_white_hair 🧑‍🦳
bald_woman 👩‍🦲
person_bald 🧑‍🦲
blond_haired_woman 👱‍♀️ blonde_woman
blond_haired_man 👱‍♂️
older_adult 🧓
frowning_man 🙍‍♂️
frowning_woman 🙍‍♀️
pouting_face 🙎
pouting_man 🙎‍♂️
pouting_woman 🙎‍♀️
no_good 🙅
no_good_man 🙅‍♂️ ng_man
no_good_woman 🙅‍♀️ ng_woman
ok_person 🙆
ok_man 🙆‍♂️
ok_woman 🙆‍♀️
tipping_hand_person 💁 information_desk_person
tipping_hand_man 💁‍♂️ sassy_man
tipping_hand_woman 💁‍♀️ sassy_woman
raising_hand_man 🙋‍♂️
raising_hand_woman 🙋‍♀️
deaf_person 🧏
deaf_man 🧏‍♂️
deaf_woman 🧏‍♀️
bowing_man 🙇‍♂️
bowing_woman 🙇‍♀️
man_facepalming 🤦‍♂️
woman_facepalming 🤦‍♀️
man_shrugging 🤷‍♂️
woman_shrugging 🤷‍♀️
health_worker 🧑‍⚕️
man_health_worker 👨‍⚕️
woman_health_worker 👩‍⚕️
student 🧑‍🎓
man_student 👨‍🎓
woman_student 👩‍🎓
teacher 🧑‍🏫
man_teacher 👨‍🏫
woman_teacher 👩‍🏫
judge 🧑‍⚖️
man_judge 👨‍⚖️
woman_judge 👩‍⚖️
farmer 🧑‍🌾
man_farmer 👨‍🌾
woman_farmer 👩‍🌾
cook 🧑‍🍳
man_cook 👨‍🍳
woman_cook 👩‍🍳
mechanic 🧑‍🔧
man_mechanic 👨‍🔧
woman_mechanic 👩‍🔧
factory_worker 🧑‍🏭
man_factory_worker 👨‍🏭
woman_factory_worker 👩‍🏭
office_worker 🧑‍💼
man_office_worker 👨‍💼
woman_office_worker 👩‍💼
scientist 🧑‍🔬
man_scientist 👨‍🔬
woman_scientist 👩‍🔬
technologist 🧑‍💻
man_technologist 👨‍💻
woman_technologist 👩‍💻
singer 🧑‍🎤
man_singer 👨‍🎤
woman_singer 👩‍🎤
artist 🧑‍🎨
man_artist 👨‍🎨
woman_artist 👩‍🎨
pilot 🧑‍✈️
man_pilot 👨‍✈️
woman_pilot 👩‍✈️
astronaut 🧑‍🚀
man_astronaut 👨‍🚀
woman_astronaut 👩‍🚀
firefighter 🧑‍🚒
man_firefighter 👨‍🚒
woman_firefighter 👩‍🚒
policeman 👮‍♂️
policewoman 👮‍♀️
male_detective 🕵️‍♂️
female_detective 🕵️‍♀️
guardswoman 💂‍♀️
construction_worker_man 👷‍♂️
construction_worker_woman 👷‍♀️
person_with_crown 🫅
person_with_turban 👳
man_with_turban 👳‍♂️
woman_with_turban 👳‍♀️
man_with_gua_pi_mao 👲
woman_with_headscarf 🧕
person_in_tuxedo 🤵
man_in_tuxedo 🤵‍♂️
woman_in_tuxedo 🤵‍♀️
person_with_veil 👰
man_with_veil 👰‍♂️
woman_with_veil 👰‍♀️ bride_with_veil
pregnant_woman 🤰
pregnant_man 🫃
pregnant_person 🫄
breast_feeding 🤱
woman_feeding_baby 👩‍🍼
man_feeding_baby 👨‍🍼
person_feeding_baby 🧑‍🍼
angel 👼
mrs_claus 🤶
mx_claus 🧑‍🎄
superhero_man 🦸‍♂️
superhero_woman 🦸‍♀️
supervillain_man 🦹‍♂️
supervillain_woman 🦹‍♀️
mage_man 🧙‍♂️
mage_woman 🧙‍♀️
fairy_man 🧚‍♂️
fairy_woman 🧚‍♀️
vampire_man 🧛‍♂️
vampire_woman 🧛‍♀️
merman 🧜‍♂️
mermaid 🧜‍♀️
elf_man 🧝‍♂️
elf_woman 🧝‍♀️
genie_man 🧞‍♂️
genie_woman 🧞‍♀️
zombie_man 🧟‍♂️
zombie_woman 🧟‍♀️
troll 🧌
massage 💆
massage_man 💆‍♂️
massage_woman 💆‍♀️
haircut 💇
haircut_man 💇‍♂️
haircut_woman 💇‍♀️
walking_man 🚶‍♂️
walking_woman 🚶‍♀️
standing_person 🧍
standing_man 🧍‍♂️
standing_woman 🧍‍♀️
kneeling_person 🧎
kneeling_man 🧎‍♂️
kneeling_woman 🧎‍♀️
person_with_probing_cane 🧑‍🦯
man_with_probing_cane 👨‍🦯
woman_with_probing_cane 👩‍🦯
person_in_motorized_wheelchair 🧑‍🦼
man_in_motorized_wheelchair 👨‍🦼
woman_in_motorized_wheelchair 👩‍🦼
person_in_manual_wheelchair 🧑‍🦽
man_in_manual_wheelchair 👨‍🦽
woman_in_manual_wheelchair 👩‍🦽
running_man 🏃‍♂️
running_woman 🏃‍♀️
business_suit_levitating 🕴️
dancers 👯
dancing_men 👯‍♂️
dancing_women 👯‍♀️
sauna_person 🧖
sauna_man 🧖‍♂️
sauna_woman 🧖‍♀️
climbing 🧗
climbing_man 🧗‍♂️
climbing_woman 🧗‍♀️
person_fencing 🤺
horse_racing 🏇
skier ⛷️
snowboarder 🏂
golfing 🏌️
golfing_man 🏌️‍♂️
golfing_woman 🏌️‍♀️
surfer 🏄
surfing_man 🏄‍♂️
surfing_woman 🏄‍♀️
rowboat 🚣
rowing_man 🚣‍♂️
rowing_woman 🚣‍♀️
swimmer 🏊
swimming_man 🏊‍♂️
swimming_woman 🏊‍♀️
bouncing_ball_person ⛹️
bouncing_ball_man ⛹️‍♂️ basketball_man
bouncing_ball_woman ⛹️‍♀️ basketball_woman
weight_lifting 🏋️
weight_lifting_man 🏋️‍♂️
weight_lifting_woman 🏋️‍♀️
bicyclist 🚴
biking_man 🚴‍♂️
biking_woman 🚴‍♀️
mountain_bicyclist 🚵
mountain_biking_man 🚵‍♂️
mountain_biking_woman 🚵‍♀️
cartwheeling 🤸
man_cartwheeling 🤸‍♂️
woman_cartwheeling 🤸‍♀️
wrestling 🤼
men_wrestling 🤼‍♂️
women_wrestling 🤼‍♀️
water_polo 🤽
man_playing_water_polo 🤽‍♂️
woman_playing_water_polo 🤽‍♀️
handball_person 🤾
man_playing_handball 🤾‍♂️
woman_playing_handball 🤾‍♀️
juggling_person 🤹
man_juggling 🤹‍♂️
woman_juggling 🤹‍♀️
lotus_position 🧘
lotus_position_man 🧘‍♂️
lotus_position_woman 🧘‍♀️
bath 🛀
sleeping_bed 🛌
two_women_holding_hands 👭
two_men_holding_hands 👬
couplekiss 💏
couplekiss_man_woman 👩‍❤️‍💋‍👨
couplekiss_man_man 👨‍❤️‍💋‍👨
couplekiss_woman_woman 👩‍❤️‍💋‍👩
couple_with_heart 💑
couple_with_heart_woman_man 👩‍❤️‍👨
couple_with_heart_man_man 👨‍❤️‍👨
couple_with_heart_woman_woman 👩‍❤️‍👩
family_man_woman_boy 👨‍👩‍👦
family_man_woman_girl 👨‍👩‍👧
family_man_woman_girl_boy 👨‍👩‍👧‍👦
family_man_woman_boy_boy 👨‍👩‍👦‍👦
family_man_woman_girl_girl 👨‍👩‍👧‍👧
family_man_man_boy 👨‍👨‍👦
family_man_man_girl 👨‍👨‍👧
family_man_man_girl_boy 👨‍👨‍👧‍👦
family_man_man_boy_boy 👨‍👨‍👦‍👦
family_man_man_girl_girl 👨‍👨‍👧‍👧
family_woman_woman_boy 👩‍👩‍👦
family_woman_woman_girl 👩‍👩‍👧
family_woman_woman_girl_boy 👩‍👩‍👧‍👦
family_woman_woman_boy_boy 👩‍👩‍👦‍👦
family_woman_woman_girl_girl 👩‍👩‍👧‍👧
family_man_boy 👨‍👦
family_man_boy_boy 👨‍👦‍👦
family_man_girl 👨‍👧
family_man_girl_boy 👨‍👧‍👦
family_man_girl_girl 👨‍👧‍👧
family_woman_boy 👩‍👦
family_woman_boy_boy 👩‍👦‍👦
family_woman_girl 👩‍👧
family_woman_girl_boy 👩‍👧‍👦
family_woman_girl_girl 👩‍👧‍👧
people_hugging 🫂
gorilla 🦍
orangutan 🦧
dog2 🐕
guide_dog 🦮
service_dog 🐕‍🦺
poodle 🐩
raccoon 🦝
cat2 🐈
black_cat 🐈‍⬛
tiger2 🐅
leopard 🐆
moose 🫎
donkey 🫏
racehorse 🐎
zebra 🦓
bison 🦬
ox 🐂
water_buffalo 🐃
cow2 🐄
pig2 🐖
pig_nose 🐽
ram 🐏
llama 🦙
mammoth 🦣
rhinoceros 🦏
hippopotamus 🦛
mouse2 🐁
rat 🐀
rabbit2 🐇
chipmunk 🐿️
beaver 🦫
hedgehog 🦔
polar_bear 🐻‍❄️
sloth 🦥
otter 🦦
skunk 🦨
badger 🦡
feet 🐾 paw_prints
turkey 🦃
rooster 🐓
hatched_chick 🐥
dove 🕊️
duck 🦆
swan 🦢
dodo 🦤
feather 🪶
flamingo 🦩
peacock 🦚
parrot 🦜
wing 🪽
black_bird 🐦‍⬛
goose 🪿
seal 🦭
shell 🐚
coral 🪸
jellyfish 🪼
beetle 🪲
cricket 🦗
cockroach 🪳
spider_web 🕸️
mosquito 🦟
fly 🪰
worm 🪱
microbe 🦠
white_flower 💮
lotus 🪷
rosette 🏵️
wilted_flower 🥀
blossom 🌼
hyacinth 🪻
potted_plant 🪴
ear_of_rice 🌾
empty_nest 🪹
nest_with_eggs 🪺
blueberries 🫐
olive 🫒
bell_pepper 🫑
leafy_green 🥬
peanuts 🥜
beans 🫘
chestnut 🌰
ginger_root 🫚
pea_pod 🫛
flatbread 🫓
bagel 🥯
cut_of_meat 🥩
tamale 🫔
stuffed_flatbread 🥙
falafel 🧆
shallow_pan_of_food 🥘
fondue 🫕
bowl_with_spoon 🥣
butter 🧈
salt 🧂
canned_food 🥫
rice_cracker 🍘
rice_ball 🍙
oden 🍢
fried_shrimp 🍤
fish_cake 🍥
moon_cake 🥮
dango 🍡
fortune_cookie 🥠
takeout_box 🥡
lobster 🦞
oyster 🦪
custard 🍮
baby_bottle 🍼
teapot 🫖
sake 🍶
pouring_liquid 🫗
cup_with_straw 🥤
bubble_tea 🧋
beverage_box 🧃
mate 🧉
ice_cube 🧊
chopsticks 🥢
plate_with_cutlery 🍽️
jar 🫙
amphora 🏺
japan 🗾
mount_fuji 🗻
desert 🏜️
national_park 🏞️
stadium 🏟️
classical_building 🏛️
building_construction 🏗️
bricks 🧱
rock 🪨
wood 🪵
hut 🛖
houses 🏘️
derelict_house 🏚️
house_with_garden 🏡
post_office 🏣
european_post_office 🏤
hotel 🏨
love_hotel 🏩
convenience_store 🏪
department_store 🏬
factory 🏭
japanese_castle 🏯
wedding 💒
church ⛪
mosque 🕌
hindu_temple 🛕
synagogue 🕍
shinto_shrine ⛩️
kaaba 🕋
fountain ⛲
foggy 🌁
cityscape 🏙️
sunrise_over_mountains 🌄
playground_slide 🛝
barber 💈
circus_tent 🎪
steam_locomotive 🚂
railway_car 🚃
bullettrain_front 🚅
metro 🚇
light_rail 🚈
station 🚉
tram 🚊
monorail 🚝
mountain_railway 🚞
oncoming_bus 🚍
trolleybus 🚎
minibus 🚐
oncoming_police_car 🚔
oncoming_taxi 🚖
oncoming_automobile 🚘
blue_car 🚙
pickup_truck 🛻
articulated_lorry 🚛
manual_wheelchair 🦽
motorized_wheelchair 🦼
auto_rickshaw 🛺
kick_scooter 🛴
skateboard 🛹
roller_skate 🛼
busstop 🚏
railway_track 🛤️
oil_drum 🛢️
wheel 🛞
traffic_light 🚥
ring_buoy 🛟
canoe 🛶
speedboat 🚤
passenger_ship 🛳️
ferry ⛴️
motor_boat 🛥️
flight_departure 🛫
flight_arrival 🛬
parachute 🪂
seat 💺
suspension_railway 🚟
mountain_cableway 🚠
aerial_tramway 🚡
artificial_satellite 🛰️
bellhop_bell 🛎️
luggage 🧳
mantelpiece_clock 🕰️
clock12 🕛
clock1230 🕧
clock130 🕜
clock2 🕑
clock230 🕝
clock3 🕒
clock330 🕞
clock4 🕓
clock430 🕟
clock5 🕔
clock530 🕠
clock6 🕕
clock630 🕡
clock7 🕖
clock730 🕢
clock8 🕗
clock830 🕣
clock9 🕘
clock930 🕤
clock10 🕙
clock1030 🕥
clock11 🕚
clock1130 🕦
waxing_crescent_moon 🌒
first_quarter_moon 🌓
moon 🌔 waxing_gibbous_moon
waning_gibbous_moon 🌖
last_quarter_moon 🌗
waning_crescent_moon 🌘
new_moon_with_face 🌚
first_quarter_moon_with_face 🌛
last_quarter_moon_with_face 🌜
thermometer 🌡️
full_moon_with_face 🌝
ringed_planet 🪐
stars 🌠
cloud_with_lightning_and_rain ⛈️
sun_behind_small_cloud 🌤️
sun_behind_large_cloud 🌥️
sun_behind_rain_cloud 🌦️
cloud_with_rain 🌧️
cloud_with_snow 🌨️
cloud_with_lightning 🌩️
fog 🌫️
wind_face 🌬️
cyclone 🌀
closed_umbrella 🌂
parasol_on_ground ⛱️
snowman_with_snow ☃️
jack_o_lantern 🎃
firecracker 🧨
tanabata_tree 🎋
dolls 🎎
flags 🎏
wind_chime 🎐
rice_scene 🎑
red_envelope 🧧
reminder_ribbon 🎗️
tickets 🎟️
medal_military 🎖️
softball 🥎
flying_disc 🥏
field_hockey 🏑
lacrosse 🥍
martial_arts_uniform 🥋
goal_net 🥅
ice_skate ⛸️
fishing_pole_and_fish 🎣
diving_mask 🤿
running_shirt_with_sash 🎽
ski 🎿
sled 🛷
curling_stone 🥌
yo_yo 🪀
kite 🪁
crystal_ball 🔮
magic_wand 🪄
teddy_bear 🧸
pinata 🪅
mirror_ball 🪩
nesting_dolls 🪆
flower_playing_cards 🎴
framed_picture 🖼️
thread 🧵
sewing_needle 🪡
yarn 🧶
knot 🪢
goggles 🥽
lab_coat 🥼
safety_vest 🦺
necktie 👔
scarf 🧣
gloves 🧤
coat 🧥
socks 🧦
sari 🥻
one_piece_swimsuit 🩱
swim_brief 🩲
shorts 🩳
womans_clothes 👚
folding_hand_fan 🪭
purse 👛
pouch 👝
shopping 🛍️
thong_sandal 🩴
hiking_boot 🥾
flat_shoe 🥿
sandal 👡
ballet_shoes 🩰
hair_pick 🪮
womans_hat 👒
billed_cap 🧢
military_helmet 🪖
rescue_worker_helmet ⛑️
prayer_beads 📿
mute 🔇
speaker 🔈
sound 🔉
loud_sound 🔊
loudspeaker 📢
mega 📣
postal_horn 📯
musical_score 🎼
studio_microphone 🎙️
level_slider 🎚️
control_knobs 🎛️
saxophone 🎷
accordion 🪗
musical_keyboard 🎹
banjo 🪕
long_drum 🪘
maracas 🪇
flute 🪈
calling 📲
phone ☎️ telephone
pager 📟
fax 📠
low_battery 🪫
trackball 🖲️
minidisc 💽
abacus 🧮
movie_camera 🎥
film_strip 🎞️
film_projector 📽️
camera_flash 📸
vhs 📼
izakaya_lantern 🏮 lantern
diya_lamp 🪔
notebook_with_decorative_cover 📔
closed_book 📕
green_book 📗
blue_book 📘
orange_book 📙
ledger 📒
page_with_curl 📃
scroll 📜
newspaper_roll 🗞️
label 🏷️
coin 🪙
yen 💴
euro 💶
pound 💷
receipt 🧾
chart 💹
incoming_envelope 📨
envelope_with_arrow 📩
mailbox_closed 📪
mailbox_with_mail 📬
mailbox_with_no_mail 📭
postbox 📮
ballot_box 🗳️
black_nib ✒️
fountain_pen 🖋️
spiral_notepad 🗒️
spiral_calendar 🗓️
card_index 📇
paperclips 🖇️
card_file_box 🗃️
file_cabinet 🗄️
lock_with_ink_pen 🔏
closed_lock_with_key 🔐
axe 🪓
hammer_and_pick ⚒️
crossed_swords ⚔️
boomerang 🪃
bow_and_arrow 🏹
carpentry_saw 🪚
screwdriver 🪛
clamp 🗜️
probing_cane 🦯
hook 🪝
ladder 🪜
alembic ⚗️
test_tube 🧪
petri_dish 🧫
dna 🧬
drop_of_blood 🩸
adhesive_bandage 🩹
crutch 🩼
stethoscope 🩺
x_ray 🩻
elevator 🛗
mirror 🪞
window 🪟
couch_and_lamp 🛋️
chair 🪑
plunger 🪠
mouse_trap 🪤
razor 🪒
lotion_bottle 🧴
safety_pin 🧷
broom 🧹
basket 🧺
roll_of_paper 🧻
bucket 🪣
soap 🧼
bubbles 🫧
toothbrush 🪥
sponge 🧽
fire_extinguisher 🧯
shopping_cart 🛒
smoking 🚬
coffin ⚰️
headstone 🪦
funeral_urn ⚱️
nazar_amulet 🧿
hamsa 🪬
placard 🪧
identification_card 🪪
atm 🏧
put_litter_in_its_place 🚮
potable_water 🚰
wheelchair ♿
mens 🚹
womens 🚺
restroom 🚻
baby_symbol 🚼
wc 🚾
passport_control 🛂
customs 🛃
baggage_claim 🛄
left_luggage 🛅
children_crossing 🚸
no_bicycles 🚳
no_smoking 🚭
do_not_litter 🚯
non-potable_water 🚱
no_pedestrians 🚷
no_mobile_phones 📵
underage 🔞
arrow_lower_right ↘️
arrow_lower_left ↙️
arrow_upper_left ↖️
arrow_up_down ↕️
left_right_arrow ↔️
leftwards_arrow_with_hook ↩️
arrow_right_hook ↪️
arrow_heading_up ⤴️
arrow_heading_down ⤵️
on 🔛
place_of_worship 🛐
orthodox_cross ☦️
star_and_crescent ☪️
menorah 🕎
six_pointed_star 🔯
khanda 🪯
aries ♈
taurus ♉
gemini ♊
cancer ♋
leo ♌
virgo ♍
libra ♎
scorpius ♏
sagittarius ♐
capricorn ♑
aquarius ♒
pisces ♓
ophiuchus ⛎
next_track_button ⏭️
arrow_backward ◀️
previous_track_button ⏮️
arrow_up_small 🔼
arrow_double_up ⏫
arrow_down_small 🔽
arrow_double_down ⏬
record_button ⏺️
eject_button ⏏️
cinema 🎦
low_brightness 🔅
high_brightness 🔆
signal_strength 📶
wireless 🛜
vibration_mode 📳
mobile_phone_off 📴
female_sign ♀️
male_sign ♂️
transgender_symbol ⚧️
heavy_division_sign ➗
heavy_equals_sign 🟰
wavy_dash 〰️
currency_exchange 💱
heavy_dollar_sign 💲
medical_symbol ⚕️
fleur_de_lis ⚜️
name_badge 📛
o ⭕
curly_loop ➰
loop ➿
part_alternation_mark 〽️
eight_pointed_black_star ✴️
asterisk *️⃣
zero 0️⃣
one 1️⃣
two 2️⃣
three 3️⃣
four 4️⃣
five 5️⃣
six 6️⃣
seven 7️⃣
eight 8️⃣
nine 9️⃣
keycap_ten 🔟
capital_abcd 🔠
abcd 🔡
1234 🔢
symbols 🔣
abc 🔤
a 🅰️
ab 🆎
b 🅱️
cl 🆑
id 🆔
m Ⓜ️
ng 🆖
o2 🅾️
parking 🅿️
vs 🆚
koko 🈁
sa 🈂️
u6708 🈷️
u6709 🈶
u6307 🈯
ideograph_advantage 🉐
u5272 🈹
u7121 🈚
u7981 🈲
accept 🉑
u7533 🈸
u5408 🈴
u7a7a 🈳
congratulations ㊗️
secret ㊙️
u55b6 🈺
u6e80 🈵
brown_circle 🟤
orange_square 🟧
yellow_square 🟨
purple_square 🟪
brown_square 🟫
black_large_square ⬛
white_large_square ⬜
black_medium_square ◼️
white_medium_square ◻️
black_medium_small_square ◾
white_medium_small_square ◽
black_small_square ▪️
white_small_square ▫️
large_orange_diamond 🔶
large_blue_diamond 🔷
small_orange_diamond 🔸
small_blue_diamond 🔹
small_red_triangle 🔺
small_red_triangle_down 🔻
diamond_shape_with_a_dot_inside 💠
radio_button 🔘
white_square_button 🔳
black_square_button 🔲
crossed_flags 🎌
transgender_flag 🏳️‍⚧️
ascension_island 🇦🇨
andorra 🇦🇩
united_arab_emirates 🇦🇪
afghanistan 🇦🇫
antigua_barbuda 🇦🇬
anguilla 🇦🇮
albania 🇦🇱
armenia 🇦🇲
angola 🇦🇴
antarctica 🇦🇶
argentina 🇦🇷
american_samoa 🇦🇸
austria 🇦🇹
aruba 🇦🇼
aland_islands 🇦🇽
azerbaijan 🇦🇿
bosnia_herzegovina 🇧🇦
barbados 🇧🇧
bangladesh 🇧🇩
belgium 🇧🇪
burkina_faso 🇧🇫
bulgaria 🇧🇬
bahrain 🇧🇭
burundi 🇧🇮
benin 🇧🇯
st_barthelemy 🇧🇱
bermuda 🇧🇲
brunei 🇧🇳
bolivia 🇧🇴
caribbean_netherlands 🇧🇶
bahamas 🇧🇸
bhutan 🇧🇹
bouvet_island 🇧🇻
botswana 🇧🇼
belarus 🇧🇾
belize 🇧🇿
cocos_islands 🇨🇨
congo_kinshasa 🇨🇩
central_african_republic 🇨🇫
congo_brazzaville 🇨🇬
switzerland 🇨🇭
cote_divoire 🇨🇮
cook_islands 🇨🇰
chile 🇨🇱
cameroon 🇨🇲
cn 🇨🇳
colombia 🇨🇴
clipperton_island 🇨🇵
costa_rica 🇨🇷
cuba 🇨🇺
cape_verde 🇨🇻
curacao 🇨🇼
christmas_island 🇨🇽
cyprus 🇨🇾
czech_republic 🇨🇿
diego_garcia 🇩🇬
djibouti 🇩🇯
denmark 🇩🇰
dominica 🇩🇲
dominican_republic 🇩🇴
algeria 🇩🇿
ceuta_melilla 🇪🇦
ecuador 🇪🇨
estonia 🇪🇪
egypt 🇪🇬
western_sahara 🇪🇭
eritrea 🇪🇷
es 🇪🇸
ethiopia 🇪🇹
finland 🇫🇮
fiji 🇫🇯
falkland_islands 🇫🇰
micronesia 🇫🇲
faroe_islands 🇫🇴
gabon 🇬🇦
grenada 🇬🇩
georgia 🇬🇪
french_guiana 🇬🇫
guernsey 🇬🇬
ghana 🇬🇭
gibraltar 🇬🇮
greenland 🇬🇱
gambia 🇬🇲
guinea 🇬🇳
guadeloupe 🇬🇵
equatorial_guinea 🇬🇶
greece 🇬🇷
south_georgia_south_sandwich_islands 🇬🇸
guatemala 🇬🇹
guam 🇬🇺
guinea_bissau 🇬🇼
guyana 🇬🇾
hong_kong 🇭🇰
heard_mcdonald_islands 🇭🇲
honduras 🇭🇳
croatia 🇭🇷
haiti 🇭🇹
hungary 🇭🇺
canary_islands 🇮🇨
indonesia 🇮🇩
israel 🇮🇱
isle_of_man 🇮🇲
british_indian_ocean_territory 🇮🇴
iraq 🇮🇶
iran 🇮🇷
iceland 🇮🇸
it 🇮🇹
jersey 🇯🇪
jamaica 🇯🇲
jordan 🇯🇴
kenya 🇰🇪
kyrgyzstan 🇰🇬
cambodia 🇰🇭
kiribati 🇰🇮
comoros 🇰🇲
st_kitts_nevis 🇰🇳
north_korea 🇰🇵
kr 🇰🇷
kuwait 🇰🇼
cayman_islands 🇰🇾
kazakhstan 🇰🇿
laos 🇱🇦
lebanon 🇱🇧
st_lucia 🇱🇨
liechtenstein 🇱🇮
sri_lanka 🇱🇰
liberia 🇱🇷
lesotho 🇱🇸
lithuania 🇱🇹
luxembourg 🇱🇺
latvia 🇱🇻
libya 🇱🇾
morocco 🇲🇦
monaco 🇲🇨
moldova 🇲🇩
montenegro 🇲🇪
st_martin 🇲🇫
madagascar 🇲🇬
marshall_islands 🇲🇭
macedonia 🇲🇰
mali 🇲🇱
myanmar 🇲🇲
mongolia 🇲🇳
macau 🇲🇴
northern_mariana_islands 🇲🇵
martinique 🇲🇶
mauritania 🇲🇷
montserrat 🇲🇸
malta 🇲🇹
mauritius 🇲🇺
maldives 🇲🇻
malawi 🇲🇼
mexico 🇲🇽
malaysia 🇲🇾
mozambique 🇲🇿
namibia 🇳🇦
new_caledonia 🇳🇨
niger 🇳🇪
norfolk_island 🇳🇫
nigeria 🇳🇬
nicaragua 🇳🇮
norway 🇳🇴
nepal 🇳🇵
nauru 🇳🇷
niue 🇳🇺
oman 🇴🇲
panama 🇵🇦
peru 🇵🇪
french_polynesia 🇵🇫
papua_new_guinea 🇵🇬
philippines 🇵🇭
pakistan 🇵🇰
poland 🇵🇱
st_pierre_miquelon 🇵🇲
pitcairn_islands 🇵🇳
puerto_rico 🇵🇷
palestinian_territories 🇵🇸
portugal 🇵🇹
palau 🇵🇼
paraguay 🇵🇾
qatar 🇶🇦
reunion 🇷🇪
romania 🇷🇴
serbia 🇷🇸
ru 🇷🇺
rwanda 🇷🇼
saudi_arabia 🇸🇦
solomon_islands 🇸🇧
seychelles 🇸🇨
sudan 🇸🇩
sweden 🇸🇪
singapore 🇸🇬
st_helena 🇸🇭
slovenia 🇸🇮
svalbard_jan_mayen 🇸🇯
slovakia 🇸🇰
sierra_leone 🇸🇱
san_marino 🇸🇲
senegal 🇸🇳
somalia 🇸🇴
suriname 🇸🇷
south_sudan 🇸🇸
sao_tome_principe 🇸🇹
el_salvador 🇸🇻
sint_maarten 🇸🇽
syria 🇸🇾
swaziland 🇸🇿
tristan_da_cunha 🇹🇦
turks_caicos_islands 🇹🇨
chad 🇹🇩
french_southern_territories 🇹🇫
togo 🇹🇬
thailand 🇹🇭
tajikistan 🇹🇯
tokelau 🇹🇰
timor_leste 🇹🇱
turkmenistan 🇹🇲
tunisia 🇹🇳
tonga 🇹🇴
tr 🇹🇷
trinidad_tobago 🇹🇹
tuvalu 🇹🇻
taiwan 🇹🇼
tanzania 🇹🇿
ukraine 🇺🇦
uganda 🇺🇬
us_outlying_islands 🇺🇲
united_nations 🇺🇳
uruguay 🇺🇾
uzbekistan 🇺🇿
vatican_city 🇻🇦
st_vincent_grenadines 🇻🇨
venezuela 🇻🇪
british_virgin_islands 🇻🇬
us_virgin_islands 🇻🇮
vietnam 🇻🇳
vanuatu 🇻🇺
wallis_futuna 🇼🇫
samoa 🇼🇸
kosovo 🇽🇰
yemen 🇾🇪
mayotte 🇾🇹
zambia 🇿🇲
zimbabwe 🇿🇼
england 🏴󠁧󠁢󠁥󠁮󠁧󠁿
scotland 🏴󠁧󠁢󠁳󠁣󠁴󠁿
wales 🏴󠁧󠁢󠁷󠁬󠁳󠁿
`

  const entries = []
  const byName = new Map()
  for (const [list, usual] of [[RAW, true], [MORE, false]]) for (const line of list.split('\n')) {
    const parts = line.trim().split(' ')
    if (parts.length < 2) continue
    const [name, char, ...aliases] = parts
    if (byName.has(name)) continue
    const entry = { name, char, aliases, usual }
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
      const hit = { name: e.name, char: e.char, alias: by === e.name ? '' : by, matched: by, usual: e.usual }
      ;(best === 0 ? starts : within).push(hit)
    }
    // The name typed in full first; then the usual ones (RAW's) before the rest; then the shortest.
    const exact = (e) => (e.matched === q ? 0 : 1)
    const rare = (e) => (e.usual ? 0 : 1)
    const order = (a, b) => exact(a) - exact(b) || rare(a) - rare(b) || a.matched.length - b.matched.length || a.name.localeCompare(b.name)
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

  function drawn(text) {
    // Looked behind, not taken: a code's own closing colon can be what stands before the next (:bug::tada:).
    return String(text).replace(/(?<![a-z0-9]):([a-z0-9_+-]+):(?![a-z0-9])/gi, (code, name) => named(name) || code)
  }

  function usual(name) {
    const e = byName.get(String(name || '').toLowerCase())
    return !!(e && e.usual)
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

  const api = { find, at, named, drawn, usual, closing, place, emoticonBefore, count: entries.length }
  if (typeof module === 'object' && module.exports) module.exports = api
  else root.vibeEmoji = api
})(this)
