using Go = import "/go.capnp";

$Go.package("aircraftlib");
$Go.import("zombiezen.com/go/capnproto2/internal/aircraftlib");

@0x832bcc6686a26d56;

const constDate :Zdate = (year = 2015, month = 8, day = 27);
const constList :List(Zdate) = [(year = 2015, month = 8, day = 27), (year = 2015, month = 8, day = 28)];
const constEnum :Airport = jfk;

struct Zdate {
  year  @0   :Int16;
  month @1   :UInt8;
  day   @2   :UInt8;
}

struct Zdata {
  data @0 :Data;
}


enum Airport {
  none @0;
  jfk @1;
  lax @2;
  sfo @3;
  luv @4;
  dfw @5;
  test @6;
  # test must be last because we use it to count
  # the number of elements in the Airport enum.
}

struct PlaneBase {
  name       @0: Text;
  homes      @1: List(Airport);
  rating     @2: Int64;
  canFly     @3: Bool;
  capacity   @4: Int64;
  maxSpeed   @5: Float64;
}

struct B737 {
  base @0: PlaneBase;
}

struct A320 {
  base @0: PlaneBase;
}

struct F16 {
  base @0: PlaneBase;
}


# need a struct with at least two pointers to catch certain bugs
struct Regression {
  base     @0: PlaneBase;
  b0       @1: Float64; # intercept
  beta     @2: List(Float64);
  planes   @3: List(Aircraft);
  ymu      @4: Float64; # y-mean in original space
  ysd      @5: Float64; # y-standard deviation in original space
}



struct Aircraft {
  #  so we can restrict
  #  and specify a Plane is required in
  #  certain places.

  union {
    void      @0: Void; # @0 will be the default, so always make @0 a Void.
    b737      @1: B737;
    a320      @2: A320;
    f16       @3: F16;
  }
}


struct Z {
  # Z must contain all types, as this is our
  # runtime type identification. It is a thin shim.

  union {
    void              @0: Void; # always first in any union.
    zz                @1: Z;    # any. fyi, this can't be 'z' alone.

    f64               @2: Float64;
    f32               @3: Float32;

    i64               @4: Int64;
    i32               @5: Int32;
    i16               @6: Int16;
    i8                @7: Int8;

    u64               @8:  UInt64;
    u32               @9:  UInt32;
    u16               @10: UInt16;
    u8                @11: UInt8;

    bool              @12: Bool;
    text              @13: Text;
    blob              @14: Data;

    f64vec            @15: List(Float64);
    f32vec            @16: List(Float32);

    i64vec            @17: List(Int64);
    i32vec            @18: List(Int32);
    i16vec            @19: List(Int16);
    i8vec             @20: List(Int8);

    u64vec            @21: List(UInt64);
    u32vec            @22: List(UInt32);
    u16vec            @23: List(UInt16);
    u8vec             @24: List(UInt8);

    boolvec           @39: List(Bool);
    datavec           @40: List(Data);
    textvec           @41: List(Text);

    zvec              @25: List(Z);
    zvecvec           @26: List(List(Z));

    zdate             @27: Zdate;
    zdata             @28: Zdata;

    aircraftvec       @29: List(Aircraft);
    aircraft          @30: Aircraft;
    regression        @31: Regression;
    planebase         @32: PlaneBase;
    airport           @33: Airport;
    b737              @34: B737;
    a320              @35: A320;
    f16               @36: F16;
    zdatevec          @37: List(Zdate);
    zdatavec          @38: List(Zdata);

    grp               :group {
      first           @42 :UInt64;
      second          @43 :UInt64;
    }

    echo              @44 :Echo;
    echoBases         @45 :EchoBases;
  }
}

# tests for Text/List(Text) recusion handling

struct Counter {
  size  @0: Int64;
  words @1: Text;
  wordlist @2: List(Text);
  bitlist @3: List(Bool);
}

struct Bag {
  counter  @0: Counter;
}

struct Zserver {
   waitingjobs       @0: List(Zjob);
}

struct Zjob {
    cmd        @0: Text;
    args       @1: List(Text);
}

# versioning test structs

struct VerEmpty {
}

struct VerOneData {
    val @0: Int16;
}

struct VerTwoData {
    val @0: Int16;
    duo @1: Int64;
}

struct VerOnePtr {
    ptr @0: VerOneData;
}

struct VerTwoPtr {
       ptr1 @0: VerOneData;
       ptr2 @1: VerOneData;
}

struct VerTwoDataTwoPtr {
    val @0: Int16;
    duo @1: Int64;
    ptr1 @2: VerOneData;
    ptr2 @3: VerOneData;
}

struct HoldsVerEmptyList {
  mylist @0: List(VerEmpty);
}

struct HoldsVerOneDataList {
  mylist @0: List(VerOneData);
}

struct HoldsVerTwoDataList {
  mylist @0: List(VerTwoData);
}

struct HoldsVerOnePtrList {
  mylist @0: List(VerOnePtr);
}

struct HoldsVerTwoPtrList {
  mylist @0: List(VerTwoPtr);
}

struct HoldsVerTwoTwoList {
  mylist @0: List(VerTwoDataTwoPtr);
}

struct HoldsVerTwoTwoPlus {
  mylist @0: List(VerTwoTwoPlus);
}

struct VerTwoTwoPlus {
    val @0: Int16;
    duo @1: Int64;
    ptr1 @2: VerTwoDataTwoPtr;
    ptr2 @3: VerTwoDataTwoPtr;
    tre  @4: Int64;
    lst3 @5: List(Int64);
}

# text handling

struct HoldsText {
       txt @0: Text;
       lst @1: List(Text);
       lstlst @2: List(List(Text));
}

# test that we avoid unnecessary truncation

struct WrapEmpty {
   mightNotBeReallyEmpty @0: VerEmpty;
}

struct Wrap2x2 {
   mightNotBeReallyEmpty @0: VerTwoDataTwoPtr;
}

struct Wrap2x2plus {
   mightNotBeReallyEmpty @0: VerTwoTwoPlus;
}

# test voids in a union

struct VoidUnion {
  union {
    a @0 :Void;
    b @1 :Void;
  }
}

# test List(List(Struct(List)))

struct Nester1Capn {
   strs  @0:   List(Text);
}

struct RWTestCapn {
   nestMatrix  @0:   List(List(Nester1Capn));
}

struct ListStructCapn {
   vec  @0:   List(Nester1Capn);
}

# test interfaces

interface Echo {
  echo @0 (in :Text) -> (out :Text);
}

struct Hoth {
  base @0 :EchoBase;
}

struct EchoBase {
  echo @0 :Echo;
}

# test List(Struct(Interface))

struct EchoBases {
	bases @0 :List(EchoBase);
}

# test transforms

struct StackingRoot {
  a @1 :StackingA;
  aWithDefault @0 :StackingA = (num = 42);
}

struct StackingA {
  num @0 :Int32;
  b @1 :StackingB;
}

struct StackingB {
  num @0 :Int32;
}

interface CallSequence {
  getNumber @0 () -> (n :UInt32);
}

# test defaults

struct Defaults {
  text @0 :Text = "foo";
  data @1 :Data = "bar";
  float @2 :Float32 = 3.14;
  int @3 :Int32 = -123;
  uint @4 :UInt32 = 42;
}

# benchmarks

struct BenchmarkA {
  name     @0 :Text;
  birthDay @1 :Int64;
  phone    @2 :Text;
  siblings @3 :Int32;
  spouse   @4 :Bool;
  money    @5 :Float64;
}

struct AllocBenchmark {
  fields @0 :List(Field);

  struct Field {
    stringValue @0 :Text;
  }
}
