@0x8ae03d633330d781;

struct KeyValue @0x8df8bc5abdc060a6 {
  key @0 :Text;
  value @1 :Value;
}

struct Value @0xd3602730c572a43b {
  union {
    void @0 :Void;
    bool @1 :Bool;
    int8 @2 :Int8;
    int16 @3 :Int16;
    int32 @4 :Int32;
    int64 @5 :Int64;
    uint8 @6 :UInt8;
    uint16 @7 :UInt16;
    uint32 @8 :UInt32;
    uint64 @9 :UInt64;
    float32 @10 :Float32;
    float64 @11 :Float64;
    text @12 :Text;
    data @13 :Data;
    cheese @29 :Cheese;

    map @14 :List(KeyValue);
    voidList @15 :List(Void);
    boolList @16 :List(Bool);
    int8List @17 :List(Int8);
    int16List @18 :List(Int16);
    int32List @19 :List(Int32);
    int64List @20 :List(Int64);
    uint8List @21 :List(UInt8);
    uint16List @22 :List(UInt16);
    uint32List @23 :List(UInt32);
    uint64List @24 :List(UInt64);
    float32List @25 :List(Float32);
    float64List @26 :List(Float64);
    textList @27 :List(Text);
    dataList @28 :List(Data);
    cheeseList @30 :List(Cheese);
    matrix @31 :List(List(Int32));
  }
}

enum Cheese {
  cheddar @0;
  gouda @1;
}

const kv @0xc0b634e19e5a9a4e :KeyValue = (key = "42", value = (int32 = -123));
const floatKv @0x967c8fe21790b0fb :KeyValue = (key = "float", value = (float64 = 3.14));
const boolKv @0xdf35cb2e1f5ea087 :KeyValue = (key = "bool", value = (bool = false));
const mapVal @0xb167974479102805 :Value = (map = [
  (key = "foo", value = (void = void)),
  (key = "bar", value = (void = void)),
]);
const data @0x8e85252144f61858 :Value = (data = 0x"4869 dead beef cafe");
const emptyMap @0x81fdbfdc91779421 :Value = (map = []);
const voidList @0xc21398a8474837ba :Value = (voidList = [void, void]);
const boolList @0xde82c2eeb3a4b07c :Value = (boolList = [true, false, true, false]);
const int8List @0xf9e3ffc179272aa2 :Value = (int8List = [1, -2, 3]);
const int64List @0xfc421b96ec6ad2b6 :Value = (int64List = [1, -2, 3]);
const uint8List @0xb3034b89d02775a5 :Value = (uint8List = [255, 0, 1]);
const uint64List @0x9246c307e46ad03b :Value = (uint64List = [1, 2, 3]);
const floatList @0xd012128a1a9cb7fc :Value = (float32List = [0.5, 3.14, -2.0]);
const textList @0xf16c386c66d492e2 :Value = (textList = ["foo", "bar", "baz"]);
const dataList @0xe14f4d42aa55de8c :Value = (dataList = [0x"deadbeef", 0x"cafe"]);
const cheese @0xe88c91698f7f0b73 :Value = (cheese = gouda);
const cheeseList @0x9c51b843b337490b :Value = (cheeseList = [gouda, cheddar]);
const matrix @0x81e2aadb8bfb237b :Value = (matrix = [[1, 2, 3], [4, 5, 6]]);

const kvList @0x90c9e81e6418df8e :List(KeyValue) = [
  (key = "foo", value = (void = void)),
  (key = "bar", value = (void = void)),
];
